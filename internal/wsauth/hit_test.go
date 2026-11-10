package wsauth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

// recDir records which path of the Directory the authenticator used.
type recDir struct {
	view             any
	hit              bool
	err              error
	cached, loadedAt int
	loadCtxDeadline  bool
	id               string
}

func (d *recDir) CachedView(id string) (any, bool, error) {
	d.cached++
	d.id = id
	return d.view, d.hit, d.err
}
func (d *recDir) LoadView(ctx context.Context, _ string) (any, error) {
	d.loadedAt++
	_, d.loadCtxDeadline = ctx.Deadline()
	return d.view, d.err
}

func TestACacheHitTakesNoDeadlineAndAllocatesNothing(t *testing.T) {
	ws, _ := (&dir{}).ByPublicID(context.Background(), curID)
	d := &recDir{view: wsauth.Resolve(ws), hit: true}
	a := wsauth.New(d, 0)
	r := authReq("/w/"+curID+"/v1/x", "198.51.100.1:1")
	if _, err := a.Authenticate(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if d.loadedAt != 0 {
		t.Fatal("a hit went down the miss path")
	}
	if n := testing.AllocsPerRun(200, func() { _, _ = a.Authenticate(context.Background(), r) }); n != 0 {
		t.Fatalf("a hit allocates %v times, want 0", n)
	}
}

func TestAMissGetsTheLookupDeadline(t *testing.T) {
	ws, _ := (&dir{}).ByPublicID(context.Background(), curID)
	d := &recDir{view: wsauth.Resolve(ws)}
	if _, err := wsauth.New(d, 0).Authenticate(context.Background(), authReq("/w/"+curID+"/x", "198.51.100.1:1")); err != nil {
		t.Fatal(err)
	}
	if d.loadedAt != 1 || !d.loadCtxDeadline {
		t.Fatalf("miss path: loads %d, deadline %v", d.loadedAt, d.loadCtxDeadline)
	}
}

func TestACachedRefusalAnswersWithoutALookup(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"unknown id remembered", contract.NewError(contract.CodeNotFound, "x"), 404},
		{"reset id remembered", contract.NewError(contract.CodeWorkspaceURLReset, "x"), 410},
	} {
		d := &recDir{hit: true, err: tc.err}
		_, err := wsauth.New(d, 0).Authenticate(context.Background(), authReq("/w/"+curID+"/x", "198.51.100.1:1"))
		if statusOf(err) != tc.want || d.loadedAt != 0 {
			t.Errorf("%s: status %d, loads %d", tc.name, statusOf(err), d.loadedAt)
		}
	}
}

func TestAViewOfTheWrongTypeIs503(t *testing.T) {
	d := &recDir{view: "not a view", hit: true}
	if _, err := wsauth.New(d, 0).Authenticate(context.Background(), authReq("/w/"+curID+"/x", "1.1.1.1:1")); statusOf(err) != 503 {
		t.Fatalf("got %v", err)
	}
}

func TestUnparsableNetworksStillRestrict(t *testing.T) {
	ws := contract.Workspace{ID: wsID, AllowedClientCIDRs: []string{"not-a-cidr"}}
	a := wsauth.New(&recDir{view: wsauth.Resolve(ws), hit: true}, 0)
	if _, err := a.Authenticate(context.Background(), authReq("/w/"+curID+"/x", "198.51.100.1:1")); statusOf(err) != 403 {
		t.Fatalf("a list with no usable network must refuse, got %v", err)
	}
}

func TestResolveCarriesTheMaxConcurrentOverrideAsAnAttr(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    any
		want string
	}{
		{"float from JSON", float64(3), "3"}, {"json.Number", json.Number("5"), "5"}, {"int", 7, "7"},
		{"clamped", float64(1e9), "1024"}, {"zero", float64(0), ""}, {"negative", float64(-2), ""},
		{"fraction", 2.5, ""}, {"string", "4", ""}, {"missing", nil, ""},
	} {
		settings := map[string]any{}
		if tc.v != nil {
			settings["maxConcurrent"] = tc.v
		}
		res := wsauth.Resolve(contract.Workspace{ID: wsID, Settings: settings}).(*wsauth.Resolved)
		if got := res.Tenant.Attrs[wsauth.AttrMaxConcurrent]; got != tc.want {
			t.Errorf("%s: attr %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDevTenantOnlyForPathsWithoutAWorkspacePrefix(t *testing.T) {
	dev := tollgate.Tenant{ID: "dev-ws"}
	plain := wsauth.New(&recDir{hit: true, err: contract.NewError(contract.CodeNotFound, "x")}, 0)
	if _, err := plain.Authenticate(context.Background(), authReq("/openai/v1/chat", "1.1.1.1:1")); statusOf(err) != 404 {
		t.Fatalf("outside dev a bare provider path is 404, got %v", err)
	}
	a := plain.WithDevTenant(dev)
	if ten, err := a.Authenticate(context.Background(), authReq("/openai/v1/chat", "1.1.1.1:1")); err != nil || ten.ID != "dev-ws" {
		t.Fatalf("dev bare path: %+v %v", ten, err)
	}
	if _, err := a.Authenticate(context.Background(), authReq("/w/short/v1/chat", "1.1.1.1:1")); statusOf(err) != 404 {
		t.Fatalf("a malformed workspace url is 404 even in dev, got %v", err)
	}
	if _, err := a.Authenticate(context.Background(), authReq("/w/"+curID+"/v1/chat", "1.1.1.1:1")); statusOf(err) != 404 {
		t.Fatalf("a workspace url goes through the directory even in dev, got %v", err)
	}
}

func TestFrontStripsThePrefixAndStashesTheID(t *testing.T) {
	d := &recDir{hit: true, err: errors.New("stop")}
	a := wsauth.New(d, 0)
	var gotPath, gotQuery, gotAuth string
	h := wsauth.Front(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotAuth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		_, _ = a.Authenticate(r.Context(), r)
	}))
	for _, tc := range []struct {
		in, path, id string
	}{
		{"/w/" + curID + "/anthropic/v1/messages?beta=1", "/anthropic/v1/messages", curID},
		{"/w/" + curID, "/", curID},
		{"/openai/v1/x", "/openai/v1/x", ""},
		{"/w/short/openai/v1/x", "/w/short/openai/v1/x", ""},
	} {
		d.id = ""
		req := httptest.NewRequest("POST", tc.in, nil)
		req.Header.Set("Authorization", "Bearer k")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if gotPath != tc.path || d.id != tc.id || gotAuth != "Bearer k" {
			t.Errorf("%s: path %q (want %q), id %q (want %q), auth %q", tc.in, gotPath, tc.path, d.id, tc.id, gotAuth)
		}
	}
	if gotQuery != "" { // the last case has no query
		t.Errorf("query %q", gotQuery)
	}
	req := httptest.NewRequest("POST", "/w/"+curID+"/openai/v1/x?beta=1", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if gotQuery != "beta=1" {
		t.Errorf("the query must survive the rewrite, got %q", gotQuery)
	}
}
