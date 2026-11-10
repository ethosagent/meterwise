package api_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ethosagent/meterwise/internal/access"
	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
)

// scopeCounter counts the per-request grant views the handlers start.
type scopeCounter struct {
	allowAuthz
	n atomic.Int32
}

func (c *scopeCounter) Scope(ctx context.Context, p contract.Principal) *access.GrantView {
	c.n.Add(1)
	return c.allowAuthz.Scope(ctx, p)
}

// GET /api/workspaces and GET /api/me resolve every permission they need from one Authz view per
// request, so a member pays for one grant read.
func TestWorkspaceListAndMeUseOneAuthzViewPerRequest(t *testing.T) {
	az := &scopeCounter{}
	f := newFixture(t, func(d *api.Deps) { d.Authz = az })
	for _, path := range []string{"/api/workspaces", "/api/me"} {
		az.n.Store(0)
		if resp, b := f.do(t, "GET", path, ""); resp.StatusCode != 200 {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, b)
		}
		if got := az.n.Load(); got != 1 {
			t.Errorf("%s started %d Authz views, want 1", path, got)
		}
	}
}
