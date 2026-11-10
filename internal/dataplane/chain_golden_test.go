package dataplane_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/ethosagent/tollgate"
	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/dataplane"
)

type fixedAuth struct{}

func (fixedAuth) Authenticate(context.Context, *http.Request) (tollgate.Tenant, error) {
	return tollgate.Tenant{ID: "ws", Attrs: map[string]string{}}, nil
}

// With trim, router and memory off (their step 0 state) the body that reaches the upstream is the
// client's body, byte for byte, on both wires and across a session whose calls repeat a tool call
// (the monitors observe it and say nothing). This is the "off is byte-identical to Plan 07" golden.
func TestEveryFeatureOffLeavesTheUpstreamBodyByteIdentical(t *testing.T) {
	for _, w := range []struct{ name, fixture, path string }{
		{"openai", "openai/chat", "/openai/v1/chat/completions"},
		{"anthropic", "anthropic/messages", "/anthropic/v1/messages"},
	} {
		t.Run(w.name, func(t *testing.T) {
			up := testkit.NewFakeUpstream(t, testkit.Options{})
			base, _ := url.Parse(up.URL)
			provs := map[string]tollgate.Provider{
				"openai":    {Name: "openai", Wire: tollgate.WireOpenAIChat, BaseURL: base},
				"anthropic": {Name: "anthropic", Wire: tollgate.WireAnthropicMessages, BaseURL: base},
			}
			hooks, _ := dataplane.HookChain(dataplane.ChainOptions{PrivacyMode: true}, nil, namedHook{name: "ledger"})
			p, err := dataplane.New(cfg(t, map[string]string{}), dataplane.Deps{
				Auth: fixedAuth{}, Resolver: dataplane.NewWorkspaceResolver(provs, true), Hooks: hooks,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := p.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(p.Handler())
			t.Cleanup(func() { srv.Close(); _ = p.Close(context.Background()) })

			_, body := testkit.RequestFor(w.fixture)
			const calls = 4
			for i := range calls {
				req, _ := http.NewRequest(http.MethodPost, srv.URL+w.path, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("x-session-id", "golden")
				req.Header.Set(testkit.FixtureHeader, w.fixture)
				resp, err := srv.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("call %d: status %d", i, resp.StatusCode)
				}
			}
			got := up.Requests()
			if len(got) != calls {
				t.Fatalf("upstream saw %d requests, want %d", len(got), calls)
			}
			for i, r := range got {
				if !bytes.Equal(r.Body, body) {
					t.Errorf("call %s: upstream body differs from the client's (%d vs %d bytes)", strconv.Itoa(i+1), len(r.Body), len(body))
				}
			}
		})
	}
}
