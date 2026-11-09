package tenancy

import (
	"context"
	"sync"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// DirectoryTTL is how long a workspace lookup is cached.
const DirectoryTTL = 30 * time.Second

const maxCacheEntries = 4096

type cached struct {
	ws  contract.Workspace
	exp time.Time
}

// Directory implements contract.WorkspaceDirectory over the store with a 30 s
// cache. Update and delete paths call Bust. Misses (unknown ids) are not
// cached, so memory is bounded by real workspaces. The cache is per process:
// a second replica would need pub/sub or a shorter TTL.
type Directory struct {
	st  *store.Store
	org string
	now func() time.Time

	mu    sync.Mutex
	byPub map[string]cached
	byID  map[string]cached
}

var _ contract.WorkspaceDirectory = (*Directory)(nil)

// NewDirectory builds the directory; now may be nil.
func NewDirectory(st *store.Store, now func() time.Time) *Directory {
	if now == nil {
		now = time.Now
	}
	return &Directory{st: st, org: store.OrgID(), now: now, byPub: map[string]cached{}, byID: map[string]cached{}}
}

// ByPublicID resolves a current or retired public id. A retired id inside its
// grace window answers with Retired set; an unknown one is CodeNotFound.
func (d *Directory) ByPublicID(ctx context.Context, publicID string) (contract.Workspace, error) {
	if ws, ok := d.hit(d.byPub, publicID); ok {
		return ws, nil
	}
	ws, err := d.st.WorkspaceByPublicID(ctx, publicID, d.now())
	if err != nil {
		return contract.Workspace{}, err
	}
	d.put(ws, true)
	return ws, nil
}

// ByID resolves a workspace by its internal id, whatever org it is in; the
// caller checks OrgID.
func (d *Directory) ByID(ctx context.Context, id string) (contract.Workspace, error) {
	if ws, ok := d.hit(d.byID, id); ok {
		return ws, nil
	}
	ws, err := d.st.WorkspaceByID(ctx, id)
	if err != nil {
		return contract.Workspace{}, err
	}
	d.put(ws, false)
	return ws, nil
}

// List returns the principal's org's workspaces (never cached: the UI reads it right after a write).
func (d *Directory) List(ctx context.Context, p contract.Principal) ([]contract.Workspace, error) {
	return d.st.ListWorkspaces(ctx, p.OrgID)
}

// Bust drops every cached entry of the workspace (current and retired ids).
func (d *Directory) Bust(workspaceID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, c := range d.byPub {
		if c.ws.ID == workspaceID {
			delete(d.byPub, k)
		}
	}
	delete(d.byID, workspaceID)
}

func (d *Directory) hit(m map[string]cached, key string) (contract.Workspace, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := m[key]
	if !ok || !d.now().Before(c.exp) {
		delete(m, key)
		return contract.Workspace{}, false
	}
	return c.ws, true
}

func (d *Directory) put(ws contract.Workspace, byPublic bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.byPub)+len(d.byID) >= maxCacheEntries {
		d.byPub, d.byID = map[string]cached{}, map[string]cached{}
	}
	c := cached{ws: ws, exp: d.now().Add(DirectoryTTL)}
	if byPublic {
		d.byPub[ws.PublicID] = c
	} else {
		d.byID[ws.ID] = c
	}
}
