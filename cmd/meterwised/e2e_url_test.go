package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

// The whole Plan 04 path on the real boot and Postgres: reset a workspace URL over HTTP, the old id
// still authenticates inside its grace window (flagged retired), a second reset and retire-previous make
// every old id a 410, the audit rows are written, and the database refuses to change them.
func TestEndToEndResetURLGraceThen410AuditRowsAreAppendOnly(t *testing.T) {
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

	st, b := e.do("POST", "/api/workspaces", "", `{"name":"Search"}`)
	e.want(201, st, b, "create")
	ws := decodeInto[contract.WorkspaceView](t, b)
	first := str(ws.PublicID)
	auth := wsauth.New(rt.svc.Directory(), 0)
	authenticate := func(id string) (map[string]string, int) {
		r := httptest.NewRequest("POST", "/w/"+id+"/v1/chat/completions", nil)
		r.RemoteAddr = "198.51.100.9:4000"
		ten, err := auth.Authenticate(ctx, r)
		if err != nil {
			se, _ := err.(*wsauth.Error)
			return nil, se.Status
		}
		return ten.Attrs, 0
	}
	if attrs, code := authenticate(first); code != 0 || attrs["url_retired"] != "" {
		t.Fatalf("current id: %v %d", attrs, code)
	}

	st, b = e.do("POST", "/api/workspaces/"+ws.ID+"/url/reset", "", `{"graceMinutes":10}`)
	e.want(200, st, b, "reset")
	second := decodeInto[contract.ResetURLResponse](t, b).ProxyURL
	second = second[strings.LastIndex(second, "/")+1:]
	if second == first {
		t.Fatal("the id did not change")
	}
	if attrs, code := authenticate(first); code != 0 || attrs["url_retired"] != "true" {
		t.Fatalf("old id in grace: %v %d", attrs, code)
	}
	if _, code := authenticate(second); code != 0 {
		t.Fatalf("new id: %d", code)
	}

	st, b = e.do("POST", "/api/workspaces/"+ws.ID+"/url/reset", "", `{"graceMinutes":0}`)
	e.want(200, st, b, "second reset")
	time.Sleep(5 * time.Millisecond)
	if attrs, code := authenticate(first); code != 0 || attrs["url_retired"] != "true" {
		t.Fatalf("the first id keeps its own grace window after a second reset: %v %d", attrs, code)
	}
	st, b = e.do("POST", "/api/workspaces/"+ws.ID+"/url/retire-previous", "", "")
	e.want(204, st, b, "retire-previous")
	if _, code := authenticate(first); code != http.StatusGone {
		t.Fatalf("first id after retire-previous: %d, want 410", code)
	}
	if _, code := authenticate(second); code != http.StatusGone {
		t.Fatalf("second id with no grace: %d, want 410", code)
	}
	if _, code := authenticate(strings.Repeat("A", 22)); code != http.StatusNotFound {
		t.Fatalf("unknown id: %d, want 404", code)
	}

	st, b = e.do("GET", "/api/audit-log?workspaceId="+ws.ID, "", "")
	e.want(200, st, b, "audit list")
	list := decodeInto[contract.AuditList](t, b)
	var resets int
	for _, it := range list.Items {
		if it.Action == contract.AuditWorkspaceURLReset {
			resets++
		}
		if strings.Contains(string(b), first) || strings.Contains(string(b), second) {
			t.Fatal("a public id is in the audit log response")
		}
	}
	if resets != 2 || len(list.Items) != 4 { // create + two resets + retire-previous
		t.Fatalf("audit items = %+v", list.Items)
	}
	for _, stmt := range []string{`UPDATE audit_log SET action = 'x'`, `DELETE FROM audit_log`, `TRUNCATE audit_log`} {
		if _, err := rt.db.Pool().Exec(ctx, stmt); err == nil {
			t.Fatalf("%s was accepted", stmt)
		}
	}
	var n int
	if err := rt.db.Pool().QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&n); err != nil || n < 3 {
		t.Fatalf("rows after the refused changes: %d %v", n, err)
	}
}
