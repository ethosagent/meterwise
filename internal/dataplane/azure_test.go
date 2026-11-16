package dataplane_test

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ethosagent/tollgate"
	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

func TestProvidersAddAzureOnlyWhenAResourceIsNamed(t *testing.T) {
	cases := []struct {
		name, resource, wantHost string
	}{
		{name: "no resource means no azure", resource: ""},
		{name: "a resource names the azure host", resource: "rudder-ai-x", wantHost: "rudder-ai-x.openai.azure.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ps := dataplane.Providers(tc.resource)
			for _, name := range []string{"openai", "anthropic", "openrouter"} {
				if _, ok := ps[name]; !ok {
					t.Errorf("%s missing", name)
				}
			}
			az, ok := ps["azure"]
			if tc.wantHost == "" {
				if ok {
					t.Fatalf("azure present without a resource: %+v", az)
				}
				return
			}
			if !ok || az.Name != "azure" || az.Wire != tollgate.WireOpenAIChat ||
				az.BaseURL.Scheme != "https" || az.BaseURL.Host != tc.wantHost || az.BaseURL.Path != "/openai" {
				t.Fatalf("azure = %+v", az)
			}
		})
	}
}

func TestAzureResolvesTheWorkspacePathOntoTheOpenAIRoot(t *testing.T) {
	res := dataplane.NewWorkspaceResolver(dataplane.Providers("rudder-ai-x"), false)
	ten := tollgate.Tenant{ID: "x", Attrs: map[string]string{wsauth.AttrPublicID: curID}}
	p, rest, err := res.Resolve(context.Background(), ten, httptest.NewRequest("POST", "/w/"+curID+"/azure/v1/chat/completions", nil))
	if err != nil || p.Name != "azure" || rest != "/v1/chat/completions" {
		t.Fatalf("%+v %q %v", p, rest, err)
	}
}

// An Azure-shaped call streams through untouched and reaches /openai/v1/chat/completions with the
// client's own api-key header, the way Azure's v1 API expects it.
func TestAzureCallStreamsWithTheClientsAPIKey(t *testing.T) {
	r := newRig(t, rigOpts{})
	const fixture, key = "openai/chat-stream-text", "azure-live-secret"
	resp, body := r.do(t, "/w/"+curID+"/azure/v1/chat/completions", fixture, map[string]string{"api-key": key})
	if resp.StatusCode != 200 {
		t.Fatalf("status %d body %.120q", resp.StatusCode, body)
	}
	if want, _ := testkit.Fixture(fixture); body != string(want) {
		t.Fatalf("the stream was altered: %d bytes, want %d", len(body), len(want))
	}
	reqs := r.up.Requests()
	if len(reqs) != 1 {
		t.Fatalf("upstream saw %d calls, want 1", len(reqs))
	}
	if got := reqs[0].Path; got != "/openai/v1/chat/completions" {
		t.Errorf("upstream path %q", got)
	}
	if got := reqs[0].Header.Get("api-key"); got != key {
		t.Errorf("upstream api-key %q, want the client's own", got)
	}
	if strings.Contains(reqs[0].Path, curID) {
		t.Error("the workspace id reached the provider")
	}
}

// azureAt is the rig's azure provider: the fake upstream with Azure's /openai root.
func azureAt(base *url.URL) tollgate.Provider {
	u := *base
	u.Path = "/openai"
	return tollgate.Provider{Name: "azure", Wire: tollgate.WireOpenAIChat, BaseURL: &u}
}
