package api_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

var base62ID = regexp.MustCompile(`^[0-9A-Za-z]{22}$`)

func TestWorkspaceLifecycle(t *testing.T) {
	s := newStack(t)
	tok := s.signInAs(t, "user_admin", "admin@example.test", contract.RoleAdmin)
	defPublic, defID := s.defaultWorkspace(t, tok)

	code, b := s.call(t, "POST", "/api/workspaces", tok, `{"name":"  Payments  "}`)
	if code != 201 {
		t.Fatalf("create: %d %s", code, b)
	}
	ws := decodeAs[contract.WorkspaceView](t, b)
	if ws.Name != "Payments" || ws.IsDefault || !base62ID.MatchString(ws.PublicID) || ws.PublicID == defPublic ||
		ws.ProxyURL != "https://proxy.test/w/"+ws.PublicID || ws.Settings == nil || ws.CreatedAt == "" {
		t.Fatalf("created workspace = %+v", ws)
	}

	// rename and settings
	code, b = s.call(t, "PATCH", "/api/workspaces/"+ws.ID, tok, `{"name":"Billing","settings":{"capture":true,"mode":"observe"}}`)
	upd := decodeAs[contract.WorkspaceView](t, b)
	if code != 200 || upd.Name != "Billing" || upd.Settings["capture"] != true || upd.PublicID != ws.PublicID {
		t.Fatalf("patch: %d %s", code, b)
	}
	code, b = s.call(t, "PATCH", "/api/workspaces/"+ws.ID, tok, `{"name":"Billing 2"}`)
	if upd = decodeAs[contract.WorkspaceView](t, b); code != 200 || upd.Settings["mode"] != "observe" {
		t.Fatalf("a name-only patch must keep the settings: %d %s", code, b)
	}

	// switch persists, Me reports it
	code, b = s.call(t, "POST", "/api/workspaces/"+ws.ID+"/switch", tok, "")
	if code != 200 || decodeAs[contract.SwitchWorkspaceResponse](t, b).ActiveWorkspaceID != ws.ID {
		t.Fatalf("switch: %d %s", code, b)
	}
	_, b = s.call(t, "GET", "/api/me", tok, "")
	if got := activeOf(decodeAs[contract.Me](t, b)); got != ws.ID {
		t.Fatalf("active workspace %s, want %s", got, ws.ID)
	}

	// delete the active one: the default takes over
	if code, b = s.call(t, "DELETE", "/api/workspaces/"+ws.ID, tok, ""); code != 204 || len(b) != 0 {
		t.Fatalf("delete: %d %s", code, b)
	}
	_, b = s.call(t, "GET", "/api/me", tok, "")
	if got := activeOf(decodeAs[contract.Me](t, b)); got != defID {
		t.Fatalf("after deleting the active workspace, active = %s, want default %s", got, defID)
	}
	code, b = s.call(t, "DELETE", "/api/workspaces/"+ws.ID, tok, "")
	wantErr(t, code, b, 404, contract.CodeNotFound)
}

func TestDefaultWorkspaceCannotBeDeleted(t *testing.T) {
	s := newStack(t)
	tok := s.signInAs(t, "user_admin", "admin@example.test", contract.RoleAdmin)
	_, defID := s.defaultWorkspace(t, tok)
	code, b := s.call(t, "DELETE", "/api/workspaces/"+defID, tok, "")
	wantErr(t, code, b, 409, contract.CodeDefaultWorkspace)
	s.defaultWorkspace(t, tok)
}

func TestMaxWorkspacesIsEnforced(t *testing.T) {
	s := newStack(t)
	tok := s.signInAs(t, "user_admin", "admin@example.test", contract.RoleAdmin)
	seen := map[string]bool{}
	for i := 1; i < maxWorkspacesInTests; i++ { // the default already counts
		code, b := s.call(t, "POST", "/api/workspaces", tok, `{"name":"w"}`)
		if code != 201 {
			t.Fatalf("create %d: %d %s", i, code, b)
		}
		seen[decodeAs[contract.WorkspaceView](t, b).PublicID] = true
	}
	code, b := s.call(t, "POST", "/api/workspaces", tok, `{"name":"one too many"}`)
	wantErr(t, code, b, 409, contract.CodeWorkspaceLimit)
	// a delete frees a slot
	_, lb := s.call(t, "GET", "/api/workspaces", tok, "")
	items := decodeAs[contract.WorkspaceList](t, lb).Items
	if len(items) != maxWorkspacesInTests || len(seen) != maxWorkspacesInTests-1 {
		t.Fatalf("%d workspaces, %d distinct ids", len(items), len(seen))
	}
	for _, w := range items {
		if !w.IsDefault {
			s.call(t, "DELETE", "/api/workspaces/"+w.ID, tok, "")
			break
		}
	}
	if code, b = s.call(t, "POST", "/api/workspaces", tok, `{"name":"fits again"}`); code != 201 {
		t.Fatalf("after a delete: %d %s", code, b)
	}
}

func TestConcurrentCreatesCannotOvershootTheLimit(t *testing.T) {
	s := newStack(t)
	tok := s.signInAs(t, "user_admin", "admin@example.test", contract.RoleAdmin)
	const n = 12
	codes := make(chan int, n)
	for range n {
		go func() { c, _ := s.call(t, "POST", "/api/workspaces", tok, `{"name":"race"}`); codes <- c }()
	}
	created := 0
	for range n {
		if <-codes == 201 {
			created++
		}
	}
	_, lb := s.call(t, "GET", "/api/workspaces", tok, "")
	if got := len(decodeAs[contract.WorkspaceList](t, lb).Items); created != maxWorkspacesInTests-1 || got != maxWorkspacesInTests {
		t.Fatalf("created %d, total %d; want %d and %d", created, got, maxWorkspacesInTests-1, maxWorkspacesInTests)
	}
}

func TestWorkspaceBodiesAreValidated(t *testing.T) {
	s := newStack(t)
	tok := s.signInAs(t, "user_admin", "admin@example.test", contract.RoleAdmin)
	_, defID := s.defaultWorkspace(t, tok)
	tests := []struct{ name, method, path, body string }{
		{"empty name", "POST", "/api/workspaces", `{"name":"   "}`},
		{"name too long", "POST", "/api/workspaces", `{"name":"` + strings.Repeat("x", 81) + `"}`},
		{"control character", "POST", "/api/workspaces", `{"name":"a\u0007b"}`},
		{"unknown field", "POST", "/api/workspaces", `{"name":"a","publicId":"x"}`},
		{"not json", "POST", "/api/workspaces", `name=a`},
		{"no body", "POST", "/api/workspaces", ``},
		{"patch blank name", "PATCH", "/api/workspaces/" + defID, `{"name":""}`},
		{"patch wrong type", "PATCH", "/api/workspaces/" + defID, `{"settings":"x"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, b := s.call(t, tc.method, tc.path, tok, tc.body)
			wantErr(t, code, b, 422, contract.CodeValidationFailed)
		})
	}
}

func TestWorkspaceIdsOutsideTheOrgAre404(t *testing.T) {
	s := newStack(t)
	tok := s.signInAs(t, "user_admin", "admin@example.test", contract.RoleAdmin)
	// The single-org index would stop a second organization, which is the point of
	// it. Lift it on this throwaway database so a workspace of another org can exist.
	ctx := context.Background()
	for _, stmt := range []string{
		`DROP INDEX organizations_single`,
		`INSERT INTO organizations (id, name) VALUES ('00000000-0000-0000-0000-0000000000ff', 'Other')`,
		`INSERT INTO workspaces (id, org_id, public_id, name) VALUES ('00000000-0000-0000-0000-0000000000aa', '00000000-0000-0000-0000-0000000000ff', 'ZZZZZZZZZZZZZZZZZZZZZZ', 'foreign')`,
	} {
		if _, err := s.db.Pool().Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	foreign := "00000000-0000-0000-0000-0000000000aa"
	for _, id := range []string{foreign, "00000000-0000-0000-0000-0000000000bb", "not-a-uuid", "' OR 1=1 --"} {
		for _, r := range [][3]string{{"PATCH", "", `{"name":"x"}`}, {"DELETE", "", ""}, {"POST", "/switch", ""}} {
			code, b := s.call(t, r[0], "/api/workspaces/"+strings.ReplaceAll(id, " ", "%20")+r[1], tok, r[2])
			wantErr(t, code, b, 404, contract.CodeNotFound)
		}
	}
	_, lb := s.call(t, "GET", "/api/workspaces", tok, "")
	for _, w := range decodeAs[contract.WorkspaceList](t, lb).Items {
		if w.ID == foreign {
			t.Fatal("a workspace of another org is listed")
		}
	}
	var name string
	if err := s.db.Pool().QueryRow(ctx, `SELECT name FROM workspaces WHERE id = $1`, foreign).Scan(&name); err != nil || name != "foreign" {
		t.Fatalf("the foreign workspace was changed or deleted: %q %v", name, err)
	}
	var active string
	_ = s.db.Pool().QueryRow(ctx, `SELECT COALESCE(preferences->>'activeWorkspaceId','') FROM users LIMIT 1`).Scan(&active)
	if active == foreign {
		t.Fatal("switch stored a foreign workspace")
	}
}

func TestOnlyOneOrganizationCanExistAndOneDefaultWorkspace(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	if _, err := s.db.Pool().Exec(ctx, `INSERT INTO organizations (id, name) VALUES ('00000000-0000-0000-0000-0000000000ff', 'Second')`); err == nil {
		t.Fatal("a second organization was inserted")
	}
	if _, err := s.db.Pool().Exec(ctx, `INSERT INTO workspaces (org_id, public_id, name, is_default) VALUES ($1, 'YYYYYYYYYYYYYYYYYYYYYY', 'dup default', true)`, store.OrgID()); err == nil {
		t.Fatal("a second default workspace was inserted")
	}
	if _, err := s.db.Pool().Exec(ctx, `INSERT INTO workspaces (org_id, public_id, name) VALUES ($1, 'too-short', 'x')`, store.OrgID()); err == nil {
		t.Fatal("a malformed public id was accepted")
	}
	// the seeded default is created once however often boot runs
	for range 3 {
		if err := s.svc.EnsureDefaults(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := s.db.Pool().QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE is_default`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("default workspaces = %d, %v", n, err)
	}
}
