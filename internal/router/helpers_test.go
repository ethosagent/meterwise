package router_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/router"
)

type ring []contract.CallSummary

func (r ring) Calls() []contract.CallSummary { return r }

// tables is the seeded default map with verified capabilities: both OpenAI targets take tools up to the cap, the
// Anthropic haiku target takes none.
func tables() contract.RouterTables {
	t, _ := router.BuildTables(
		[]contract.RoutePair{
			{Provider: "openai", From: "gpt-4.1", To: "gpt-4.1-mini"},
			{Provider: "openai", From: "gpt-4o", To: "gpt-4o-mini"},
			{Provider: "anthropic", From: "claude-opus-4-5", To: "claude-haiku-4-5"},
			{Provider: "anthropic", From: "claude-opus-5-5", To: "claude-sonnet-5-5"},
		},
		[]router.CapRow{
			{Provider: "openai", Model: "gpt-4.1-mini", JSONSchema: true, Vision: true},
			{Provider: "openai", Model: "gpt-4o-mini", JSONSchema: true, Vision: true},
			{Provider: "anthropic", Model: "claude-haiku-4-5", MaxTools: new(0), Vision: true, Reasoning: true},
			{Provider: "anthropic", Model: "claude-sonnet-5-5", Vision: true, Reasoning: true},
		},
		allPriced{}, time.Unix(0, 0))
	return t
}

type allPriced struct{}

func (allPriced) Cost(string, string, contract.Usage, time.Time) (contract.MicroUSD, bool) {
	return 1, true
}

type pricedOnly map[string]bool

func (p pricedOnly) Cost(provider, model string, _ contract.Usage, _ time.Time) (contract.MicroUSD, bool) {
	return 1, p[provider+"/"+model]
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func attr(mode contract.RouterMode) string { return contract.RouterConfig{Mode: mode}.Attr() }

const openaiBody = `{"model":"gpt-4.1","stream":true,"messages":[{"role":"system","content":"s"},{"role":"user","content":"hi"}]}`

// callAt builds the call a hook sees at index n of its session: the ring says the previous turn read a file.
func callAt(n int, model, body string, a string, prevTools ...string) *tollgate.Call {
	if len(prevTools) == 0 {
		prevTools = []string{"read_file"}
	}
	wire, prov := tollgate.WireOpenAIChat, "openai"
	if strings.HasPrefix(model, "claude") {
		wire, prov = tollgate.WireAnthropicMessages, "anthropic"
	}
	return &tollgate.Call{
		ID: fmt.Sprintf("c%d", n), Tenant: tollgate.Tenant{ID: "ws1", Attrs: map[string]string{router.AttrRouter: a}},
		Session:  tollgate.Session{ID: "s1", TenantID: "ws1", Calls: n},
		Provider: tollgate.Provider{Name: prov, Wire: wire}, Model: model, Body: []byte(body),
		Meta: map[string]any{
			contract.MetaRing:     ring{{Index: n - 1, Tools: prevTools}},
			contract.MetaMonitors: &contract.CallVerdicts{},
		},
	}
}

func decision(t *testing.T, c *tollgate.Call) contract.RouteDecision {
	t.Helper()
	d, ok := c.Meta[contract.MetaRouter].(contract.RouteDecision)
	if !ok {
		t.Fatalf("Meta[router] = %#v, want a RouteDecision", c.Meta[contract.MetaRouter])
	}
	return d
}

func before(t *testing.T, h *router.Hook, c *tollgate.Call) contract.RouteDecision {
	t.Helper()
	resp, err := h.Before(t.Context(), c)
	if resp != nil || err != nil {
		t.Fatalf("Before = %v, %v: the router never short-circuits", resp, err)
	}
	return decision(t, c)
}
