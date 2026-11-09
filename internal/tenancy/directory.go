package tenancy

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// DirectoryTTL is how long a workspace lookup is cached.
const DirectoryTTL = 30 * time.Second

const maxCacheEntries = 4096

// cached is one lookup result. err is set only for a reset (past-grace) id, which is cached so a
// client still using the old URL does not reach Postgres on every call.
type cached struct {
	ws  contract.Workspace
	err error
	exp time.Time
}

// Directory implements contract.WorkspaceDirectory over the store with a 30 s
// cache. Update, delete and reset paths call Bust. Misses (unknown ids) are not
// cached, so memory is bounded by real workspaces; a retired id is cached no
// longer than its grace window. The cache is per process: a second replica
// would need pub/sub or a shorter TTL.
type Directory struct {
	st   *store.Store
	org  string
	now  func() time.Time
	rand io.Reader

	mu    sync.Mutex
	byPub map[string]cached
	byID  map[string]cached
	// seq counts busts; bustedAt[workspace] is the seq of its latest bust. A lookup remembers seq
	// before it reads Postgres and does not cache what it read if the workspace was busted since:
	// the row may predate the change the bust announced.
	seq      uint64
	bustedAt map[string]uint64
}

var _ contract.WorkspaceDirectory = (*Directory)(nil)

// NewDirectory builds the directory; now and rand may be nil (time.Now, crypto/rand).
func NewDirectory(st *store.Store, now func() time.Time, rand io.Reader) *Directory {
	if now == nil {
		now = time.Now
	}
	return &Directory{st: st, org: store.OrgID(), now: now, rand: rand, byPub: map[string]cached{}, byID: map[string]cached{}, bustedAt: map[string]uint64{}}
}

// ByPublicID resolves a current or retired public id. A retired id inside its
// grace window answers with Retired set, one past its grace is
// CodeWorkspaceURLReset and an unknown one is CodeNotFound.
func (d *Directory) ByPublicID(ctx context.Context, publicID string) (contract.Workspace, error) {
	if c, ok := d.hit(d.byPub, publicID); ok {
		return c.ws, c.err
	}
	seen := d.seqNow()
	ws, err := d.st.WorkspaceByPublicID(ctx, publicID, d.now())
	if err != nil {
		if contract.AsError(err).Code == contract.CodeWorkspaceURLReset {
			d.putAt(d.byPub, publicID, cached{err: err, ws: ws}, d.now().Add(DirectoryTTL), seen)
		}
		return contract.Workspace{}, err
	}
	exp := d.now().Add(DirectoryTTL)
	if ws.Retired && ws.GraceUntil != nil && ws.GraceUntil.Before(exp) {
		exp = *ws.GraceUntil
	}
	d.putAt(d.byPub, publicID, cached{ws: ws}, exp, seen)
	return ws, nil
}

// ByID resolves a workspace by its internal id, whatever org it is in; the
// caller checks OrgID.
func (d *Directory) ByID(ctx context.Context, id string) (contract.Workspace, error) {
	if c, ok := d.hit(d.byID, id); ok {
		return c.ws, nil
	}
	seen := d.seqNow()
	ws, err := d.st.WorkspaceByID(ctx, id)
	if err != nil {
		return contract.Workspace{}, err
	}
	d.putAt(d.byID, id, cached{ws: ws}, d.now().Add(DirectoryTTL), seen)
	return ws, nil
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
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, m := range []map[string]cached{d.byPub, d.byID} {
		for k, c := range m {
			if !now.Before(c.exp) || (c.ws.Retired && c.ws.GraceUntil != nil && !now.Before(*c.ws.GraceUntil)) {
				delete(m, k)
				n++
			}
		}
	}
	return n
}

// Bust drops every cached entry of the workspace (current and retired ids).
func (d *Directory) Bust(workspaceID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seq++
	d.bustedAt[workspaceID] = d.seq
	for k, c := range d.byPub {
		if c.ws.ID == workspaceID {
			delete(d.byPub, k)
		}
	}
	delete(d.byID, workspaceID)
}

func (d *Directory) hit(m map[string]cached, key string) (cached, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := m[key]
	if !ok || !d.now().Before(c.exp) {
		delete(m, key)
		return cached{}, false
	}
	return c, true
}

func (d *Directory) seqNow() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.seq
}

// putAt caches c unless its workspace was busted after the lookup began (seen is the seq then);
// a row read before a reset, retire or delete must not outlive the bust that followed it.
func (d *Directory) putAt(m map[string]cached, key string, c cached, exp time.Time, seen uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if at, ok := d.bustedAt[c.ws.ID]; (ok && at > seen) || (c.ws.ID == "" && d.seq > seen) {
		return
	}
	if len(d.byPub)+len(d.byID) >= maxCacheEntries {
		clear(d.byPub)
		clear(d.byID)
	}
	c.exp = exp
	m[key] = c
}
