package api_test

import (
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Every non-public route is behind the one gateway: no token is 401, an authenticated stranger is
// 403 not_invited (PermSelf routes aside, which decide in their handler).
func TestEveryProtectedRouteIsBehindTheGateway(t *testing.T) {
	s := newStack(t)
	stranger := s.token("user_stranger", "stranger@example.test")
	n := 0
	for _, r := range contract.Routes() {
		if r.Public {
			continue
		}
		path := strings.NewReplacer("{id}", "00000000-0000-0000-0000-000000000042", "{userId}", "00000000-0000-0000-0000-000000000043", "{wsId}", "00000000-0000-0000-0000-000000000044").Replace(r.Pattern)
		code, b := s.call(t, r.Method, path, "", "")
		wantErr(t, code, b, 401, contract.CodeUnauthenticated)
		n++
		if r.Permission == contract.PermSelf {
			continue // the caller's own data: decided in the handler (membership is not needed)
		}
		code, b = s.call(t, r.Method, path, stranger, `{}`)
		wantErr(t, code, b, 403, contract.CodeNotInvited)
	}
	if n < 30 {
		t.Fatalf("only %d protected routes were exercised", n)
	}
}

func TestForbiddenBodyCarriesTheReason(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	_, defID := s.defaultWorkspace(t, admin.tok)
	code, b := s.call(t, "PATCH", "/api/workspaces/"+defID, m.tok, `{"name":"x"}`)
	forbidden(t, code, b, "no matching grant")
	code, b = s.call(t, "POST", "/api/groups", m.tok, `{"name":"x"}`)
	forbidden(t, code, b, "admin required")
}

// A workspace outside the org is 404 for everyone; in the org but ungranted it is 403.
func TestUnknownWorkspaceIs404AndUngrantedIs403(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	_, defID := s.defaultWorkspace(t, admin.tok)
	const ghost = "00000000-0000-0000-0000-00000000dead"
	for _, tok := range []string{admin.tok, m.tok} {
		for _, r := range []struct{ method, path, body string }{
			{"PATCH", "/api/workspaces/" + ghost, `{"name":"x"}`},
			{"DELETE", "/api/workspaces/" + ghost, ""},
			{"POST", "/api/workspaces/" + ghost + "/switch", ""},
			{"POST", "/api/workspaces/not-a-uuid/switch", ""},
		} {
			code, b := s.call(t, r.method, r.path, tok, r.body)
			wantErr(t, code, b, 404, contract.CodeNotFound)
		}
	}
	code, b := s.call(t, "DELETE", "/api/workspaces/"+defID, m.tok, "")
	forbidden(t, code, b, "no matching grant")
}

// The same Authorizer, asked about a real workspace as a principal of another org, answers 404 (the
// schema holds one org, so a second tenant cannot be created through the API).
func TestAWorkspaceOfAnotherOrgIsNotFound(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, defID := s.defaultWorkspace(t, admin.tok)
	az := newAuthorizer(s)
	for _, role := range []contract.Role{contract.RoleAdmin, contract.RoleMember} {
		p := contract.Principal{UserID: admin.id, OrgID: "00000000-0000-0000-0000-0000000000ff", Role: role}
		_, err := az.Authorize(t.Context(), p, contract.PermWorkspaceRead, defID)
		if contract.AsError(err).Code != contract.CodeNotFound {
			t.Errorf("%s: %v", role, err)
		}
	}
}
