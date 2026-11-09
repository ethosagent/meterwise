package tenancy

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// DirectoryTTL is how long a workspace lookup is cached.
const DirectoryTTL = 30 * time.Second

const (
	// maxCacheEntries bounds the positive cache; at capacity the oldest entry is evicted.
	maxCacheEntries = 4096
	// maxNegativeEntries and negativeTTL bound the cache of ids that named no workspace.
	maxNegativeEntries = 1024
	negativeTTL        = 5 * time.Second
	// loadCeiling bounds one Postgres read shared by every caller waiting on the same id. A caller
	// stops waiting when its own context ends; the read itself is not tied to any one caller.
	loadCeiling = 10 * time.Second
)

// Directory implements contract.WorkspaceDirectory over the store with a 30 s cache. Update,
// delete and reset paths call Bust. The read path takes a read lock only. Concurrent misses on one
// id share one Postgres read (singleflight). An id that names no workspace is remembered for 5 s
// in a bounded negative cache; a reset id past its grace is cached like a hit; a retired id is
// cached no longer than its grace window. The cache is per process: a second replica would need
// pub/sub or a shorter TTL.
type Directory struct {
	st   *store.Store
	org  string
	now  func() time.Time
	rand io.Reader

	c    *dirCache
	neg  *negCache
	view atomic.Pointer[func(contract.Workspace) any]

	fmu     sync.Mutex
	flights map[ckey]*flight

	loads, coalesced atomic.Uint64
}

// flight is one Postgres read that every concurrent miss on the same key waits for. seen is the
// cache generation it began under: a caller that arrives after a bust does not join it, because the
// row it reads may predate the change.
type flight struct {
	done chan struct{}
	seen uint64
	e    *centry
	err  error
}

var _ contract.WorkspaceDirectory = (*Directory)(nil)

// NewDirectory builds the directory; now and rand may be nil (time.Now, crypto/rand).
func NewDirectory(st *store.Store, now func() time.Time, rand io.Reader) *Directory {
	if now == nil {
		now = time.Now
	}
	return &Directory{st: st, org: store.OrgID(), now: now, rand: rand,
		c: newDirCache(maxCacheEntries), neg: newNegCache(maxNegativeEntries, negativeTTL), flights: map[ckey]*flight{}}
}

// SetView registers the function that turns a workspace into the data plane's prepared form
// (tenant, parsed networks). It runs once per cache fill, so a hit allocates nothing. Call it
// before serving; a nil function clears it.
func (d *Directory) SetView(build func(contract.Workspace) any) {
	if build == nil {
		d.view.Store(nil)
		return
	}
	d.view.Store(&build)
}

func (d *Directory) buildView(ws contract.Workspace) any {
	if f := d.view.Load(); f != nil {
		return (*f)(ws)
	}
	return nil
}

// ByPublicID resolves a current or retired public id. A retired id inside its
// grace window answers with Retired set, one past its grace is
// CodeWorkspaceURLReset and an unknown one is CodeNotFound.
func (d *Directory) ByPublicID(ctx context.Context, publicID string) (contract.Workspace, error) {
	e, err := d.lookup(ctx, ckey{kindPublic, publicID})
	if err != nil {
		return contract.Workspace{}, err
	}
	return e.ws, nil
}

// ByID resolves a workspace by its internal id, whatever org it is in; the
// caller checks OrgID.
func (d *Directory) ByID(ctx context.Context, id string) (contract.Workspace, error) {
	e, err := d.lookup(ctx, ckey{kindID, id})
	if err != nil {
		return contract.Workspace{}, err
	}
	return e.ws, nil
}

// CachedView answers from the cache alone: no Postgres, no goroutine, no allocation. ok is false
// when the cache cannot answer (a miss, or a hit filled before SetView). A cached refusal (reset
// past grace, unknown id) comes back as ok with the error.
func (d *Directory) CachedView(publicID string) (view any, ok bool, err error) {
	now := d.now()
	if e, hit := d.c.get(ckey{kindPublic, publicID}, now); hit {
		if e.err != nil {
			return nil, true, e.err
		}
		return e.view, e.view != nil, nil
	}
	if n, hit := d.neg.get(publicID, now); hit {
		return nil, true, n.err
	}
	return nil, false, nil
}

// LoadView is CachedView's miss path: one shared Postgres read bounded by ctx.
func (d *Directory) LoadView(ctx context.Context, publicID string) (any, error) {
	e, err := d.lookup(ctx, ckey{kindPublic, publicID})
	if err != nil {
		return nil, err
	}
	if e.view != nil {
		return e.view, nil
	}
	return d.buildView(e.ws), nil
}

// lookup answers from the cache, else joins or starts the one Postgres read for k and waits for it
// or for ctx.
func (d *Directory) lookup(ctx context.Context, k ckey) (*centry, error) {
	now := d.now()
	if e, ok := d.c.get(k, now); ok {
		return e, e.err
	}
	if k.kind == kindPublic {
		if n, ok := d.neg.get(k.id, now); ok {
			return nil, n.err
		}
	}
	f, leader := d.join(k)
	if leader {
		go d.run(f, k)
	} else {
		d.coalesced.Add(1)
	}
	select {
	case <-f.done:
		return f.e, f.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *Directory) join(k ckey) (f *flight, leader bool) {
	d.fmu.Lock()
	defer d.fmu.Unlock()
	seen := d.c.generation()
	if cur, ok := d.flights[k]; ok && cur.seen == seen {
		return cur, false
	}
	f = &flight{done: make(chan struct{}), seen: seen}
	d.flights[k] = f
	return f, true
}

// run reads Postgres once for the flight, files the answer in the right cache and wakes the waiters.
func (d *Directory) run(f *flight, k ckey) {
	ctx, cancel := context.WithTimeout(context.Background(), loadCeiling)
	defer cancel()
	d.loads.Add(1)
	var (
		ws  contract.Workspace
		err error
	)
	if k.kind == kindPublic {
		ws, err = d.st.WorkspaceByPublicID(ctx, k.id, d.now())
	} else {
		ws, err = d.st.WorkspaceByID(ctx, k.id)
	}
	f.e, f.err = d.fill(k, ws, err, f.seen)
	d.fmu.Lock()
	if d.flights[k] == f {
		delete(d.flights, k)
	}
	d.fmu.Unlock()
	close(f.done)
}

// fill files a store answer: a workspace (or a reset id) in the positive cache, an unknown id in the
// negative one. seen is the generation the read began under.
func (d *Directory) fill(k ckey, ws contract.Workspace, err error, seen uint64) (*centry, error) {
	now := d.now()
	if err != nil {
		if k.kind != kindPublic {
			return nil, err
		}
		switch contract.AsError(err).Code {
		case contract.CodeWorkspaceURLReset:
			e := &centry{ws: ws, err: err, exp: now.Add(DirectoryTTL)}
			d.c.put(k, e, seen)
			return e, err
		case contract.CodeNotFound:
			d.neg.put(k.id, err, now)
		}
		return nil, err
	}
	exp := now.Add(DirectoryTTL)
	if ws.Retired && ws.GraceUntil != nil && ws.GraceUntil.Before(exp) {
		exp = *ws.GraceUntil
	}
	e := &centry{ws: ws, exp: exp, view: d.buildView(ws)}
	d.c.put(k, e, seen)
	return e, nil
}

// List returns the principal's org's workspaces (never cached: the UI reads it right after a write).
func (d *Directory) List(ctx context.Context, p contract.Principal) ([]contract.Workspace, error) {
	return d.st.ListWorkspaces(ctx, p.OrgID)
}

// Reset gives the workspace a new public id (a fresh CSPRNG draw, redrawn on a collision) and keeps
// the old one answering for grace. The cache entries of the workspace and of both ids are dropped.
func (d *Directory) Reset(ctx context.Context, p contract.Principal, workspaceID string, grace time.Duration) (contract.URLReset, error) {
	var out contract.URLReset
	for range maxIDAttempts {
		pid, err := NewPublicID(d.rand)
		if err != nil {
			return contract.URLReset{}, err
		}
		ws, until, err := d.st.ResetWorkspaceURL(ctx, p.OrgID, workspaceID, pid, grace, d.now())
		if errors.Is(err, store.ErrPublicIDTaken) {
			continue
		}
		if err != nil {
			return contract.URLReset{}, err
		}
		out = contract.URLReset{Workspace: ws, PreviousGraceUntil: until}
		d.Bust(workspaceID)
		return out, nil
	}
	return contract.URLReset{}, contract.NewError(contract.CodeInternal, "could not allocate a workspace id")
}

// RetirePrevious ends the grace of every retired id of the workspace at once and busts the cache.
func (d *Directory) RetirePrevious(ctx context.Context, p contract.Principal, workspaceID string) error {
	if _, err := d.st.EndPreviousURLGrace(ctx, p.OrgID, workspaceID, d.now()); err != nil {
		return err
	}
	d.Bust(workspaceID)
	return nil
}

// SweepRetired drops entries that are past their expiry or retired and past their grace, and
// reports how many it dropped. A retired id's entry never outlives its grace, so this is memory
// hygiene for ids nobody asks about any more, not a correctness step.
func (d *Directory) SweepRetired(now time.Time) int {
	return d.c.sweep(now) + d.neg.sweep(now)
}

// Bust drops every cached entry of the workspace (current and retired ids), in time proportional
// to the workspace's own entries, and marks the cache generation so a lookup that began before the
// bust cannot store what it read.
func (d *Directory) Bust(workspaceID string) { d.c.bust(workspaceID, d.now()) }

// Status implements contract.StatusSource: cache occupancy and read counters for /statusz.
func (d *Directory) Status() map[string]any {
	entries, evicted := d.c.size()
	return map[string]any{
		"entries": entries, "negative_entries": d.neg.size(), "evicted": evicted,
		"loads": d.loads.Load(), "coalesced": d.coalesced.Load(),
	}
}
