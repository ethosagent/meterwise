package api_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/access"
	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
)

// committedTenancy commits every workspace change; the response view is built afterwards.
type committedTenancy struct{ *memTenancy }

var committedWS = contract.Workspace{ID: "00000000-0000-0000-0000-0000000000bb", Name: "Search", PublicID: "AAAAAAAAAAAAAAAAAAAAAA", CreatedAt: time.Unix(0, 0)}

func (committedTenancy) CreateWorkspace(context.Context, contract.Principal, string) (contract.Workspace, error) {
	return committedWS, nil
}

func (committedTenancy) UpdateWorkspace(context.Context, contract.Principal, string, contract.UpdateWorkspaceRequest) (contract.Workspace, error) {
	return committedWS, nil
}

func (committedTenancy) ResetURL(context.Context, contract.Principal, string, time.Duration) (contract.URLReset, error) {
	return contract.URLReset{Workspace: committedWS, PreviousGraceUntil: time.Unix(60, 0)}, nil
}

type failingGrants struct{}

func (failingGrants) Grants(context.Context, string, string) ([]access.Grant, error) {
	return nil, errors.New("grant read failed")
}

// failingViewAuthz admits the call at the gateway but fails the grant read the response view needs
// (the caller is a plain member there, so the view must read grants).
type failingViewAuthz struct{ allowAuthz }

func (failingViewAuthz) Scope(ctx context.Context, _ contract.Principal) *access.GrantView {
	return (&access.Authorizer{Repo: failingGrants{}}).Scope(ctx, contract.Principal{UserID: "u-1", OrgID: "o-1", Role: contract.RoleMember})
}

// The audit row is written once the mutation committed, even when building the response view fails
// afterwards: the client sees the error, the log still has the change.
func TestAuditRowIsWrittenForACommittedChangeEvenWhenTheResponseViewFails(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		action                   contract.AuditAction
	}{
		{"create", "POST", "/api/workspaces", `{"name":"Search"}`, contract.AuditWorkspaceCreate},
		{"update", "PATCH", "/api/workspaces/" + committedWS.ID, `{"name":"Search"}`, contract.AuditWorkspaceUpdate},
		{"reset", "POST", "/api/workspaces/" + committedWS.ID + "/url/reset", `{"graceMinutes":5}`, contract.AuditWorkspaceURLReset},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &memAudit{}
			f := newFixture(t, func(d *api.Deps) {
				d.Audit, d.Tenancy, d.Authz = a, committedTenancy{newMemTenancy()}, failingViewAuthz{}
			})
			resp, b := f.do(t, tc.method, tc.path, tc.body)
			if resp.StatusCode < 500 {
				t.Fatalf("the failing view read answered %d %s", resp.StatusCode, b)
			}
			if len(a.events) != 1 || a.events[0].Action != tc.action || a.events[0].ResourceID != committedWS.ID {
				t.Fatalf("audit events = %+v", a.events)
			}
			if strings.Contains(string(b), committedWS.PublicID) {
				t.Fatalf("the error body carries the public id: %s", b)
			}
		})
	}
}
