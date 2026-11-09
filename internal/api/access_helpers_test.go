package api_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ethosagent/meterwise/internal/access"
	"github.com/ethosagent/meterwise/internal/contract"
)

// who is a signed-in person: their token, user id and email.
type who struct{ tok, id, email string }

// must calls the API and fails unless the status is want; it returns the body.
func (s *stack) must(t *testing.T, tok, method, path, body string, want int) []byte {
	t.Helper()
	code, b := s.call(t, method, path, tok, body)
	if code != want {
		t.Fatalf("%s %s: %d %s, want %d", method, path, code, b, want)
	}
	return b
}

func (s *stack) meOf(t *testing.T, tok string) contract.Me {
	t.Helper()
	return decodeAs[contract.Me](t, s.must(t, tok, "GET", "/api/me", "", 200))
}

// admin signs in an admin through the bootstrap invitation (the production first-admin path).
func (s *stack) admin(t *testing.T, sub string) who {
	t.Helper()
	email := sub + "@example.test"
	tok := s.signInAs(t, sub, email, contract.RoleAdmin)
	return who{tok: tok, id: s.meOf(t, tok).User.ID, email: email}
}

// invited signs in a person through a real invitation made by inviter (the production path:
// invite, then complete-signup with their own token).
func (s *stack) invited(t *testing.T, inviter, sub string, role contract.Role) who {
	t.Helper()
	email := sub + "@example.test"
	s.must(t, inviter, "POST", "/api/invitations", `{"email":"`+email+`","role":"`+string(role)+`"}`, 201)
	tok := s.token(sub, email)
	s.must(t, tok, "POST", "/api/auth/complete-signup", "", 200)
	return who{tok: tok, id: s.meOf(t, tok).User.ID, email: email}
}

func (s *stack) newWorkspace(t *testing.T, tok, name string) string {
	t.Helper()
	return decodeAs[contract.WorkspaceView](t, s.must(t, tok, "POST", "/api/workspaces", `{"name":"`+name+`"}`, 201)).ID
}

func (s *stack) newGroup(t *testing.T, tok, name string) string {
	t.Helper()
	return decodeAs[contract.Group](t, s.must(t, tok, "POST", "/api/groups", `{"name":"`+name+`"}`, 201)).ID
}

// count runs a scalar SQL count against the test database.
func (s *stack) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.Pool().QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func grantsJSON(gs ...contract.Grant) string {
	b, _ := json.Marshal(contract.GrantList{Grants: gs})
	return string(b)
}

func g(ws, rt, act string) contract.Grant {
	gr := contract.Grant{ResourceType: rt, Action: act}
	if ws != "" {
		gr.WorkspaceID = &ws
	}
	return gr
}

func grantList(t *testing.T, b []byte) []contract.Grant {
	t.Helper()
	return decodeAs[contract.GrantList](t, b).Grants
}

func hasGrant(list []contract.Grant, ws, rt, act string) bool {
	for _, x := range list {
		if x.ResourceType == rt && x.Action == act && ((ws == "" && x.WorkspaceID == nil) || (x.WorkspaceID != nil && *x.WorkspaceID == ws)) {
			return true
		}
	}
	return false
}

// forbidden asserts a 403 forbidden with details.reason.
func forbidden(t *testing.T, code int, b []byte, reason string) {
	t.Helper()
	e := decodeErr(t, b)
	if code != 403 || e.Code != contract.CodeForbidden || e.Details["reason"] != reason {
		t.Fatalf("got %d %s, want 403 forbidden reason %q", code, b, reason)
	}
}

func newAuthorizer(s *stack) *access.Authorizer { return access.NewAuthorizer(s.db) }
