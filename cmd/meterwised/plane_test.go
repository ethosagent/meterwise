package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/store"
)

func TestRoleGuardRefusesSuperusersAndAuditLogOwnersOutsideDev(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string
		rc   store.RoleCheck
		want string // "" = allowed
	}{
		{"prod superuser", "prod", store.RoleCheck{Role: "postgres", Superuser: true}, `"postgres" is a superuser`},
		{"prod owner", "prod", store.RoleCheck{Role: "owner", OwnsAuditLog: true}, `"owner" owns audit_log`},
		{"test env is not dev", "test", store.RoleCheck{Role: "x", Superuser: true}, "superuser"},
		{"prod app role", "prod", store.RoleCheck{Role: "meterwise_app"}, ""},
		{"dev superuser", "dev", store.RoleCheck{Role: "meterwise", Superuser: true, OwnsAuditLog: true}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := requireLeastPrivilegeRole(tc.env, tc.rc)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Message, tc.want) || !strings.Contains(strings.Join(err.Details, " "), "database-roles.md")):
				t.Fatalf("got %+v, want a refusal containing %q that points at the runbook", err, tc.want)
			}
		})
	}
}

func TestBootRefusesASuperuserDatabaseRoleOutsideDev(t *testing.T) {
	cfg := bootGuardConfig("prod", true)
	cfg.DatabaseURL = migratedDB(t) // the test server's role is a superuser and owns audit_log
	_, cerr := boot(context.Background(), cfg, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), dataplane.Deps{})
	if cerr == nil || cerr.Code != contract.CodeConfigInvalid || !strings.Contains(cerr.Message, "superuser") {
		t.Fatalf("got %+v", cerr)
	}
}

func TestServeThroughTheWorkspaceURLEndToEnd(t *testing.T) {
	const fixture = "openai/chat-stream-text"
	up := testkit.NewFakeUpstream(t, testkit.Options{})
	base, _ := url.Parse(up.URL)
	cfg := config.Config{
		Env: "dev", DatabaseURL: migratedDB(t), ListenProxy: "127.0.0.1:0", ListenAPI: "127.0.0.1:0", ListenAdmin: "127.0.0.1:0",
		MaxWorkspaces: 3, WebDir: t.TempDir(), MaxConcurrentPerWorkspace: 4, PrivacyMode: true,
	}
	var logs syncBuf
	ctx, cancel := context.WithCancel(context.Background())
	rt, cerr := boot(ctx, cfg, slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})), dataplane.Deps{Resolver: fakeResolver{base}})
	if cerr != nil {
		t.Fatal(cerr)
	}
	done := make(chan error, 1)
	go func() { done <- rt.serve(ctx) }()
	stop := sync.OnceFunc(func() { cancel(); <-done })
	t.Cleanup(func() { stop(); rt.close(context.Background()) })
	proxy, apiURL, admin := "http://"+rt.lns["proxy"].Addr().String(), "http://"+rt.lns["api"].Addr().String(), "http://"+rt.lns["admin"].Addr().String()

	list, err := rt.svc.Directory().List(ctx, contract.Principal{OrgID: store.OrgID()})
	if err != nil || len(list) == 0 {
		t.Fatalf("workspaces: %v %v", list, err)
	}
	ws := list[0]
	post := func(path string) (int, []byte) {
		_, body := testkit.RequestFor(fixture)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, proxy+path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer sk-caller-own-key")
		req.Header.Set(testkit.FixtureHeader, fixture)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		return resp.StatusCode, b.Bytes()
	}
	want, _ := testkit.Fixture(fixture)
	if code, got := post("/w/" + ws.PublicID + "/openai/v1/chat/completions"); code != 200 || !bytes.Equal(got, want) {
		t.Fatalf("workspace url: %d, %d bytes", code, len(got))
	}
	if reqs := up.Requests(); len(reqs) != 1 || reqs[0].Header.Get("Authorization") != "Bearer sk-caller-own-key" || reqs[0].Path != "/v1/chat/completions" {
		t.Fatalf("upstream saw %+v", reqs)
	}
	if code, _ := post("/w/" + strings.Repeat("A", 22) + "/openai/v1/chat/completions"); code != 404 {
		t.Fatalf("unknown id: %d", code)
	}

	// reset with no grace: the old url is 410, the new one streams
	res, err := rt.svc.Directory().Reset(ctx, contract.Principal{OrgID: store.OrgID(), Role: contract.RoleAdmin}, ws.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := post("/w/" + ws.PublicID + "/openai/v1/chat/completions"); code != 410 {
		t.Fatalf("reset id after grace: %d", code)
	}
	if code, _ := post("/w/" + res.Workspace.PublicID + "/openai/v1/chat/completions"); code != 200 {
		t.Fatalf("new id: %d", code)
	}
	// dev only: a bare provider path serves the default workspace
	if code, _ := post("/openai/v1/chat/completions"); code != 200 {
		t.Fatalf("dev bare path: %d", code)
	}

	// /statusz carries every source
	code, body := get(t, admin+"/statusz")
	var doc struct {
		Sources map[string]map[string]any `json:"sources"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &doc) != nil || doc.Sources["admission"]["default_max"] != float64(4) || doc.Sources["directory"]["loads"] == nil {
		t.Fatalf("statusz %d %s", code, body)
	}

	// L4: touch the API with a workspace-shaped path, then grep every log line for either public id
	_, _ = get(t, apiURL+"/w/"+res.Workspace.PublicID+"/x")
	stop()
	out := logs.String()
	for _, id := range []string{ws.PublicID, res.Workspace.PublicID} {
		if strings.Contains(out, id) {
			t.Fatalf("a log line carries the public id %q:\n%s", id, out)
		}
	}
	if !strings.Contains(out, "/w/<redacted>") {
		t.Fatalf("expected a redacted workspace path in the logs:\n%s", out)
	}
}
