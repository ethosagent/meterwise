package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/identity/clerkmock"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

// e2e is the whole binary's boot against Postgres and the clerkmock twin: real
// listeners, real signed JWTs, the CLI and the HTTP API on one database.
type e2e struct {
	t    *testing.T
	api  string
	mock *clerkmock.Server
	dsn  string
}

func (e *e2e) do(method, path, token, body string) (int, []byte) {
	e.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, e.api+path, strings.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (e *e2e) want(status int, code int, body []byte, what string) {
	e.t.Helper()
	if code != status {
		e.t.Fatalf("%s: status %d, want %d: %s", what, code, status, body)
	}
}

func TestEndToEndInviteFirstLoginMembershipWorkspaceSwitchDelete(t *testing.T) {
	dsn := migratedDB(t)
	mock, err := clerkmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)

	// 1. the CLI seeds the first admin invitation (no Clerk configured: database only, as in dev)
	var out, errb bytes.Buffer
	code := runCtx(context.Background(), []string{"bootstrap-admin", "--email", "Founder@Example.test", "-json"},
		lookup(map[string]string{"DATABASE_URL": dsn, "APP_ENV": "dev"}), &out, &errb)
	r := decode(t, out.String())
	if code != 0 || !r.OK || r.Data["created"] != "true" || r.Data["email"] != "founder@example.test" || r.Data["role"] != "admin" {
		t.Fatalf("bootstrap-admin: %d %s %s", code, out.String(), errb.String())
	}

	// 2. boot with the Clerk adapter pointed at the twin
	cfg := config.Config{
		Env: "dev", DatabaseURL: dsn, ListenProxy: "127.0.0.1:0", ListenAPI: "127.0.0.1:0", ListenAdmin: "127.0.0.1:0",
		ProxyPublicHost: "proxy.example.com", AppName: "Acme", MaxWorkspaces: 3, WebDir: t.TempDir(),
		Clerk: config.Clerk{Issuer: mock.Issuer(), JWKSURL: mock.JWKSURL(), SecretKey: clerkmock.BackendKey(), WebhookSecret: mock.WebhookSecret(), APIURL: mock.APIURL()},
	}
	ctx, cancel := context.WithCancel(context.Background())
	rt, cerr := boot(ctx, cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)), dataplane.Deps{})
	if cerr != nil {
		t.Fatal(cerr)
	}
	done := make(chan error, 1)
	go func() { done <- rt.serve(ctx) }()
	t.Cleanup(func() { cancel(); <-done; rt.close(context.Background()) })
	e := &e2e{t: t, api: "http://" + rt.lns["api"].Addr().String(), mock: mock, dsn: dsn}

	founder := mock.Token(clerkmock.Claims{Subject: "user_founder", Email: "founder@example.test", Name: "Founder"})
	member := mock.Token(clerkmock.Claims{Subject: "user_member", Email: "member@example.test", Name: "Member"})

	// 3. an uninvited person is told so; the invited admin's first login makes the membership
	st, b := e.do("GET", "/api/me", member, "")
	e.want(403, st, b, "uninvited /api/me")
	st, b = e.do("POST", "/api/auth/complete-signup", founder, "")
	e.want(200, st, b, "founder complete-signup")
	me := decodeInto[contract.Me](t, b)
	if me.Role != contract.RoleAdmin || me.ActiveWorkspaceID == nil {
		t.Fatalf("founder me = %+v", me)
	}
	defaultWS := *me.ActiveWorkspaceID

	// 4. the admin invites a member; Clerk is asked to send the email
	st, b = e.do("POST", "/api/invitations", founder, `{"email":"Member@Example.test","role":"member"}`)
	e.want(201, st, b, "invite")
	if inv := mock.Invitations(); len(inv) != 1 || inv[0].Email != "member@example.test" {
		t.Fatalf("Clerk invitations: %+v", inv)
	}

	// 5. the member's first login creates their membership; they cannot use admin routes
	st, b = e.do("POST", "/api/auth/complete-signup", member, "")
	e.want(200, st, b, "member complete-signup")
	if got := decodeInto[contract.Me](t, b); got.Role != contract.RoleMember || str(got.ActiveWorkspaceID) != defaultWS {
		t.Fatalf("member me = %+v", got)
	}
	st, b = e.do("GET", "/api/invitations", member, "")
	e.want(403, st, b, "member on an admin route")

	// 6. workspace create, switch, delete
	st, b = e.do("POST", "/api/workspaces", member, `{"name":"Search team"}`)
	e.want(201, st, b, "create workspace")
	ws := decodeInto[contract.WorkspaceView](t, b)
	if ws.PublicID == nil || ws.ProxyURL == nil || len(*ws.PublicID) != 22 || *ws.ProxyURL != "https://proxy.example.com/w/"+*ws.PublicID {
		t.Fatalf("workspace = %+v", ws)
	}
	st, b = e.do("POST", "/api/workspaces/"+ws.ID+"/switch", member, "")
	e.want(200, st, b, "switch")
	_, b = e.do("GET", "/api/me", member, "")
	if got := str(decodeInto[contract.Me](t, b).ActiveWorkspaceID); got != ws.ID {
		t.Fatalf("active = %s, want %s", got, ws.ID)
	}
	// the founder's own active workspace is unaffected by the member's switch
	_, b = e.do("GET", "/api/me", founder, "")
	if got := str(decodeInto[contract.Me](t, b).ActiveWorkspaceID); got != defaultWS {
		t.Fatalf("founder active = %s, want the default %s", got, defaultWS)
	}
	// ruling: create is open to members; rename and delete are admin-only
	st, b = e.do("PATCH", "/api/workspaces/"+ws.ID, member, `{"name":"Hijacked"}`)
	e.want(403, st, b, "member rename")
	st, b = e.do("DELETE", "/api/workspaces/"+ws.ID, member, "")
	e.want(403, st, b, "member delete")
	st, b = e.do("DELETE", "/api/workspaces/"+ws.ID, founder, "")
	e.want(204, st, b, "admin deletes the workspace")
	st, b = e.do("DELETE", "/api/workspaces/"+defaultWS, founder, "")
	e.want(409, st, b, "delete default workspace")
	_, b = e.do("GET", "/api/me", member, "")
	if got := str(decodeInto[contract.Me](t, b).ActiveWorkspaceID); got != defaultWS {
		t.Fatalf("after deleting the active workspace: %s, want default %s", got, defaultWS)
	}

	// 7. the database holds exactly what the flow implies
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var members, accepted, workspaces, users int
	for q, dst := range map[string]*int{
		`SELECT count(*) FROM memberships WHERE status = 'active'`:       &members,
		`SELECT count(*) FROM invitations WHERE accepted_at IS NOT NULL`: &accepted,
		`SELECT count(*) FROM workspaces`:                                &workspaces,
		`SELECT count(*) FROM users`:                                     &users,
	} {
		if err := db.Pool().QueryRow(ctx, q).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	if members != 2 || accepted != 2 || workspaces != 1 || users != 2 {
		t.Fatalf("members=%d accepted=%d workspaces=%d users=%d, want 2 2 1 2", members, accepted, workspaces, users)
	}
}

func decodeInto[T any](t *testing.T, b []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("decode %T from %q: %v", v, b, err)
	}
	return v
}

func TestBootstrapAdminIsIdempotentAndValidatesTheEmail(t *testing.T) {
	kv := map[string]string{"DATABASE_URL": migratedDB(t), "APP_ENV": "dev"}
	for i, want := range []string{"true", "false"} {
		code, out, errb := exec(t, kv, "bootstrap-admin", "--email=admin@example.test", "-json")
		r := decode(t, out)
		if code != 0 || r.Data["created"] != want {
			t.Fatalf("run %d: %d %s %s", i, code, out, errb)
		}
	}
	for _, args := range [][]string{
		{"bootstrap-admin", "-json"}, {"bootstrap-admin", "--email", "", "-json"}, {"bootstrap-admin", "--email", "not-an-email", "-json"},
		{"bootstrap-admin", "--email"},
	} {
		if code, _, _ := exec(t, kv, args...); code == 0 {
			t.Errorf("%v accepted", args)
		}
	}
	// an unmigrated database fails with the typed hint, not a stack of SQL
	code, out, _ := exec(t, map[string]string{"DATABASE_URL": pgtest.NewDatabase(t), "APP_ENV": "dev"}, "bootstrap-admin", "--email", "a@example.test", "-json")
	if code != 1 || decode(t, out).Error.Code != contract.CodeUnavailable || !strings.Contains(out, "migrate") {
		t.Fatalf("unmigrated: %d %s", code, out)
	}
}

// In dev without Clerk the stub resolver signs in a real, seeded dev admin, so the web app
// runs against the same tables and code paths as production.
func TestDevBootWithoutClerkSignsInASeededAdminThroughTheRealTables(t *testing.T) {
	cfg := config.Config{
		Env: "dev", DatabaseURL: migratedDB(t), ListenProxy: "127.0.0.1:0", ListenAPI: "127.0.0.1:0", ListenAdmin: "127.0.0.1:0",
		ProxyPublicHost: "proxy.example.com", AppName: "Acme", MaxWorkspaces: 3, WebDir: t.TempDir(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	rt, cerr := boot(ctx, cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)), dataplane.Deps{})
	if cerr != nil {
		t.Fatal(cerr)
	}
	done := make(chan error, 1)
	go func() { done <- rt.serve(ctx) }()
	t.Cleanup(func() { cancel(); <-done; rt.close(context.Background()) })
	e := &e2e{t: t, api: "http://" + rt.lns["api"].Addr().String()}
	st, b := e.do("GET", "/api/me", "", "")
	e.want(200, st, b, "dev /api/me")
	me := decodeInto[contract.Me](t, b)
	if me.Role != contract.RoleAdmin || me.User.Email != "dev-admin@meterwise.local" || me.ActiveWorkspaceID == nil {
		t.Fatalf("dev me = %+v", me)
	}
	st, b = e.do("POST", "/api/workspaces", "", `{"name":"dev ws"}`)
	e.want(201, st, b, "dev create workspace")
}

// str dereferences a nullable id; "" when null.
func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
