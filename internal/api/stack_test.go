package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/access"
	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/identity"
	"github.com/ethosagent/meterwise/internal/identity/clerkmock"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
	"github.com/ethosagent/meterwise/internal/tenancy"
)

// stack is the real control plane: Postgres, the tenancy service, the Clerk
// adapter and the clerkmock twin that signs real JWTs against a local JWKS.
type stack struct {
	srv  *httptest.Server
	mock *clerkmock.Server
	db   *store.Store
	svc  *tenancy.Service
	dsn  string
	logs *bytes.Buffer
}

const maxWorkspacesInTests = 3

func newStack(t *testing.T) *stack {
	t.Helper()
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(context.Background(), dsn); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	mock, err := clerkmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	idp, err := identity.NewClerk(identity.ClerkConfig{Issuer: mock.Issuer(), JWKSURL: mock.JWKSURL(), SecretKey: clerkmock.BackendKey(), APIURL: mock.APIURL()})
	if err != nil {
		t.Fatal(err)
	}
	svc := tenancy.New(db, idp, tenancy.Config{MaxWorkspaces: maxWorkspacesInTests})
	if err := svc.EnsureDefaults(context.Background()); err != nil {
		t.Fatal(err)
	}
	wh, err := identity.NewWebhookVerifier(mock.WebhookSecret(), nil)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	h, err := api.New(api.Deps{
		Log: slog.New(slog.NewJSONHandler(logs, nil)), DB: db,
		Principals: api.ClerkPrincipalResolver{Provider: idp, Sessions: svc}, Tenancy: svc, Authz: access.NewAuthorizer(db, db), Access: access.NewService(db, store.OrgID()), Webhooks: wh,
		AppName: "Acme", ProxyPublicHost: "proxy.test", Version: contract.Version{Version: "t"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &stack{mock: mock, db: db, svc: svc, dsn: dsn, logs: logs}
	s.srv = httptest.NewServer(h)
	t.Cleanup(s.srv.Close)
	return s
}

// call sends a request; token "" sends no Authorization header.
func (s *stack) call(t *testing.T, method, path, token, body string) (int, []byte) {
	t.Helper()
	return s.callWith(t, method, path, body, func(r *http.Request) {
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
	})
}

func (s *stack) callWith(t *testing.T, method, path, body string, edit func(*http.Request)) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, s.srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(req)
	}
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// token signs a valid session token for a user.
func (s *stack) token(sub, email string) string {
	return s.mock.Token(clerkmock.Claims{Subject: sub, Email: email, Name: "Test " + sub})
}

// signInAs makes email an active member with role: it records the invitation the
// way the admin API does (bootstrap for admin), then runs complete-signup with a real token.
func (s *stack) signInAs(t *testing.T, sub, email string, role contract.Role) string {
	t.Helper()
	ctx := context.Background()
	if role == contract.RoleAdmin {
		if _, _, err := s.svc.BootstrapAdmin(ctx, email); err != nil {
			t.Fatal(err)
		}
	}
	tok := s.token(sub, email)
	if role != contract.RoleAdmin {
		sess, err := s.svc.Resolve(ctx, contract.Identity{Subject: sub, Email: email, EmailVerified: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.svc.EnsureMember(ctx, sess.User.ID, role); err != nil {
			t.Fatal(err)
		}
		return tok
	}
	if code, b := s.call(t, "POST", "/api/auth/complete-signup", tok, ""); code != 200 {
		t.Fatalf("complete-signup for %s: %d %s", email, code, b)
	}
	return tok
}

func decodeAs[T any](t *testing.T, b []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("decode %T from %q: %v", v, b, err)
	}
	return v
}

func wantErr(t *testing.T, status int, body []byte, wantStatus int, code contract.Code) {
	t.Helper()
	e := decodeErr(t, body)
	if status != wantStatus || e.Code != code {
		t.Fatalf("got %d %s, want %d %s", status, body, wantStatus, code)
	}
}

// activeOf is the active workspace id of a Me, "" when it is null.
func activeOf(me contract.Me) string {
	if me.ActiveWorkspaceID == nil {
		return ""
	}
	return *me.ActiveWorkspaceID
}
