package router_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/router"
)

func TestOffWritesNothingAndTouchesNothing(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	for _, a := range []string{"", attr(contract.RouterModeOff), "not json"} {
		c := callAt(5, "gpt-4.1", openaiBody, a)
		if resp, err := h.Before(t.Context(), c); resp != nil || err != nil {
			t.Fatal(resp, err)
		}
		if _, stamped := c.Meta[contract.MetaRouter]; stamped || string(c.Body) != openaiBody || c.Model != "gpt-4.1" {
			t.Errorf("attr %q: off must leave the call alone: meta %v body %s model %s", a, c.Meta, c.Body, c.Model)
		}
	}
}

func TestPassthroughWireIsIgnored(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	c := callAt(5, "gpt-4.1", openaiBody, attr(contract.RouterModeConservative))
	c.Provider.Wire = tollgate.WirePassthrough
	_, _ = h.Before(t.Context(), c)
	if _, stamped := c.Meta[contract.MetaRouter]; stamped || string(c.Body) != openaiBody {
		t.Fatal("a passthrough path is never routed")
	}
}

func TestConservativeRoutesOnlyTheModelField(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	c := callAt(5, "gpt-4.1", openaiBody, attr(contract.RouterModeConservative))
	d := before(t, h, c)
	want := contract.RouteDecision{Mode: contract.RouterModeConservative, Action: contract.RouteRouted, From: "gpt-4.1", To: "gpt-4.1-mini", ScoreBp: 2500}
	if d != want {
		t.Fatalf("decision = %+v, want %+v", d, want)
	}
	if got, wantBody := string(c.Body), strings.Replace(openaiBody, `"model":"gpt-4.1"`, `"model":"gpt-4.1-mini"`, 1); got != wantBody {
		t.Errorf("body = %s\nwant   %s", got, wantBody)
	}
	if c.Model != "gpt-4.1-mini" {
		t.Errorf("c.Model = %q: the ledger prices the call at the target (F3)", c.Model)
	}
}

func TestDatedSourceMatchesItsBaseAndTheTargetIsSentExactly(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	for _, model := range []string{"gpt-4.1-2025-04-14", "gpt-4.1-latest"} {
		c := callAt(5, model, strings.Replace(openaiBody, "gpt-4.1", model, 1), attr(contract.RouterModeConservative))
		d := before(t, h, c)
		if d.Action != contract.RouteRouted || d.From != model || d.To != "gpt-4.1-mini" || !strings.Contains(string(c.Body), `"model":"gpt-4.1-mini"`) {
			t.Errorf("%s: %+v body %s", model, d, c.Body)
		}
	}
	// a look-alike is a different model: gpt-4.1-mini is not a gpt-4.1 and has no pair of its own
	c := callAt(5, "gpt-4.1-mini", strings.Replace(openaiBody, "gpt-4.1", "gpt-4.1-mini", 1), attr(contract.RouterModeConservative))
	if d := before(t, h, c); d.Action != contract.RouteHeld || d.Reason != contract.HeldNotInMap {
		t.Errorf("gpt-4.1-mini: %+v", d)
	}
}

func TestObserveRecordsWouldRouteAndRewritesNothing(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	c := callAt(5, "gpt-4.1", openaiBody, attr(contract.RouterModeObserve))
	d := before(t, h, c)
	if d.Action != contract.RouteObserve || d.To != "gpt-4.1-mini" || d.From != "gpt-4.1" || d.ScoreBp != 2500 {
		t.Fatalf("decision = %+v", d)
	}
	if string(c.Body) != openaiBody || c.Model != "gpt-4.1" {
		t.Errorf("observe must not edit: body %s model %s", c.Body, c.Model)
	}
}

func TestHeldReasons(t *testing.T) {
	cons := attr(contract.RouterModeConservative)
	cases := []struct {
		name   string
		call   func() *tollgate.Call
		reason string
	}{
		{"first call", func() *tollgate.Call { return callAt(1, "gpt-4.1", openaiBody, cons) }, contract.HeldFirstSteps},
		{"second call", func() *tollgate.Call { return callAt(2, "gpt-4.1", openaiBody, cons) }, contract.HeldFirstSteps},
		{"a write step is not simple", func() *tollgate.Call { return callAt(5, "gpt-4.1", openaiBody, cons, "edit_file") }, router.HeldScore},
		{"a model with no pair", func() *tollgate.Call {
			return callAt(5, "gpt-5.2", strings.Replace(openaiBody, "gpt-4.1", "gpt-5.2", 1), cons)
		}, contract.HeldNotInMap},
		{"a failing tool result", func() *tollgate.Call {
			c := callAt(5, "gpt-4.1", openaiBody, cons)
			c.Messages = []tollgate.Message{{Role: tollgate.RoleAssistant}, {Role: tollgate.RoleTool, Text: "Traceback (most recent call last):\n  File x"}}
			return c
		}, contract.HeldErrorRetry},
		{"a source file that merely mentions errors still routes", nil, ""},
	}
	h := router.New(router.Options{Tables: tables()})
	for _, tc := range cases {
		if tc.call == nil {
			c := callAt(5, "gpt-4.1", openaiBody, cons)
			c.Messages = []tollgate.Message{{Role: tollgate.RoleAssistant}, {Role: tollgate.RoleTool, Text: "func f() error {\n\treturn errors.New(\"error: x\")\n}\n// error handling\n"}}
			if d := before(t, h, c); d.Action != contract.RouteRouted {
				t.Errorf("%s: %+v", tc.name, d)
			}
			continue
		}
		c := tc.call()
		d := before(t, h, c)
		if d.Action != contract.RouteHeld || d.Reason != tc.reason {
			t.Errorf("%s: decision %+v, want held %s", tc.name, d, tc.reason)
		}
		if tc.reason != contract.HeldNotInMap && c.Model != "gpt-4.1" && c.Model != "gpt-5.2" || strings.Contains(string(c.Body), "-mini") {
			t.Errorf("%s: a held call keeps its model: %s %s", tc.name, c.Model, c.Body)
		}
	}
}

func TestUnpricedPairIsNeverRouted(t *testing.T) {
	routes := []contract.RoutePair{{Provider: "openai", From: "gpt-4.1", To: "gpt-4.1-mini"}}
	caps := []router.CapRow{{Provider: "openai", Model: "gpt-4.1-mini"}}
	for name, priced := range map[string]pricedOnly{
		"target unpriced": {"openai/gpt-4.1": true},
		"source unpriced": {"openai/gpt-4.1-mini": true},
	} {
		tb, dropped := router.BuildTables(routes, caps, priced, time.Unix(0, 0))
		if len(tb.Routes) != 0 || len(dropped) != 1 {
			t.Fatalf("%s: tables %+v dropped %+v", name, tb.Routes, dropped)
		}
		// a workspace map that names the dropped pair still cannot route it: Priced says no
		a := contract.RouterConfig{Mode: contract.RouterModeConservative, Map: routes}.Attr()
		h := router.New(router.Options{Tables: tb})
		if d := before(t, h, callAt(5, "gpt-4.1", openaiBody, a)); d.Action != contract.RouteHeld || d.Reason != contract.HeldUnpriced {
			t.Errorf("%s: %+v", name, d)
		}
	}
}

func TestBuildTablesDropsPairsAndResolvesNullMaxTools(t *testing.T) {
	routes := []contract.RoutePair{
		{Provider: "openai", From: "gpt-4.1", To: "gpt-4.1-mini"},
		{Provider: "openai", From: "gpt-4o", To: "gpt-4o-mini"}, // target has no capability row
		{Provider: "anthropic", From: "x", To: "y"},             // nothing priced
	}
	caps := []router.CapRow{{Provider: "openai", Model: "gpt-4.1-mini", MaxTools: nil}, {Provider: "anthropic", Model: "claude-haiku-4-5", MaxTools: new(0)}, {Provider: "openai", Model: "big", MaxTools: new(99)}}
	pr := pricedOnly{"openai/gpt-4.1": true, "openai/gpt-4.1-mini": true, "openai/gpt-4o": true, "openai/gpt-4o-mini": true}
	tb, dropped := router.BuildTables(routes, caps, pr, time.Unix(0, 0))
	if len(tb.Routes) != 1 || tb.Routes[0].From != "gpt-4.1" {
		t.Errorf("routes = %+v", tb.Routes)
	}
	want := map[string]string{"openai/gpt-4o→gpt-4o-mini": router.DropNoCaps, "anthropic/x→y": router.DropFromUnpriced}
	for _, d := range dropped {
		k := d.Pair.Provider + "/" + d.Pair.From + "→" + d.Pair.To
		if want[k] != d.Reason {
			t.Errorf("dropped %s for %q, want %q", k, d.Reason, want[k])
		}
		delete(want, k)
	}
	if len(want) != 0 {
		t.Errorf("pairs not dropped: %v", want)
	}
	if got := tb.Caps["openai/gpt-4.1-mini"].MaxTools; got != router.DefaultMaxTools {
		t.Errorf("NULL max_tools = %d, want the router constant %d", got, router.DefaultMaxTools)
	}
	if got := tb.Caps["anthropic/claude-haiku-4-5"].MaxTools; got != 0 {
		t.Errorf("max_tools 0 must stay 0 (no tool use): %d", got)
	}
	if got := tb.Caps["openai/big"].MaxTools; got != 99 {
		t.Errorf("an explicit max is kept: %d", got)
	}
	if !tb.Priced["openai/gpt-4.1"] || tb.Priced["anthropic/x"] {
		t.Errorf("priced = %v", tb.Priced)
	}
}

// The router's tool-count cap is a code constant applied when the capability row states no maximum.
func TestToolCountCapAppliesWhenMaxToolsIsNull(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	tools := func(n int) string {
		return `{"model":"gpt-4.1","tools":[` + strings.TrimSuffix(strings.Repeat(`{"type":"function"},`, n), ",") + `],"messages":[]}`
	}
	cons := attr(contract.RouterModeConservative)
	if d := before(t, h, callAt(5, "gpt-4.1", tools(router.DefaultMaxTools), cons)); d.Action != contract.RouteRouted {
		t.Errorf("%d tools: %+v", router.DefaultMaxTools, d)
	}
	if d := before(t, h, callAt(5, "gpt-4.1", tools(router.DefaultMaxTools+1), cons)); d.Action != contract.RouteHeld || d.Reason != contract.HeldCapability {
		t.Errorf("%d tools: %+v", router.DefaultMaxTools+1, d)
	}
}

func TestACallWithToolsIsNeverRoutedToAModelWithMaxToolsZero(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	cons := attr(contract.RouterModeConservative)
	withTools := `{"model":"claude-opus-4-5","max_tokens":9,"tools":[{"name":"a"}],"messages":[]}`
	c := callAt(5, "claude-opus-4-5", withTools, cons)
	if d := before(t, h, c); d.Action != contract.RouteHeld || d.Reason != contract.HeldCapability || string(c.Body) != withTools {
		t.Errorf("tools to a max_tools 0 target: %+v body %s", d, c.Body)
	}
	plain := `{"model":"claude-opus-4-5","max_tokens":9,"messages":[]}`
	c = callAt(5, "claude-opus-4-5", plain, cons)
	if d := before(t, h, c); d.Action != contract.RouteRouted || !strings.Contains(string(c.Body), `"model":"claude-haiku-4-5"`) {
		t.Errorf("without tools the same pair routes: %+v body %s", d, c.Body)
	}
}

func TestCapabilityGuardOnBothWires(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	cons := attr(contract.RouterModeConservative)
	const imgOA = `{"model":"gpt-4.1","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"u"}}]}]}`
	cases := []struct {
		name, model, body string
		held              bool
	}{
		{"openai json_schema routes to a target that supports it", "gpt-4.1", `{"model":"gpt-4.1","response_format":{"type":"json_schema"},"messages":[]}`, false},
		{"openai image routes to a vision target", "gpt-4.1", imgOA, false},
		{"openai reasoning_effort is held (target has no reasoning)", "gpt-4.1", `{"model":"gpt-4.1","reasoning_effort":"high","messages":[]}`, true},
		{"anthropic json output is held (target has no schema support)", "claude-opus-4-5", `{"model":"claude-opus-4-5","output_format":{"type":"json_schema"},"messages":[]}`, true},
		{"anthropic thinking routes to a target with reasoning", "claude-opus-4-5", `{"model":"claude-opus-4-5","thinking":{"type":"enabled","budget_tokens":1024},"messages":[]}`, false},
		{"anthropic image routes to a vision target", "claude-opus-4-5", `{"model":"claude-opus-4-5","messages":[{"role":"user","content":[{"type":"image","source":{}}]}]}`, false},
		{"anthropic sonnet target without schema holds output_config.format", "claude-opus-5-5", `{"model":"claude-opus-5-5","output_config":{"format":{"type":"json_schema"}},"messages":[]}`, true},
		{"a body the router cannot read is held", "gpt-4.1", `{"model":"gpt-4.1","tools":[{`, true},
	}
	for _, tc := range cases {
		c := callAt(5, tc.model, tc.body, cons)
		d := before(t, h, c)
		if tc.held != (d.Action == contract.RouteHeld) || (tc.held && d.Reason != contract.HeldCapability) {
			t.Errorf("%s: %+v", tc.name, d)
		}
		if tc.held && string(c.Body) != tc.body {
			t.Errorf("%s: a held call's body is untouched", tc.name)
		}
	}
	// the strict openai target: no json schema, no vision, no reasoning
	strict := router.New(router.Options{Tables: func() contract.RouterTables {
		tb, _ := router.BuildTables([]contract.RoutePair{{Provider: "openai", From: "gpt-4.1", To: "gpt-4.1-mini"}},
			[]router.CapRow{{Provider: "openai", Model: "gpt-4.1-mini"}}, allPriced{}, time.Unix(0, 0))
		return tb
	}()})
	for _, body := range []string{
		`{"model":"gpt-4.1","response_format":{"type":"json_schema"},"messages":[]}`, imgOA,
		`{"model":"gpt-4.1","reasoning_effort":"low","messages":[]}`,
	} {
		if d := before(t, strict, callAt(5, "gpt-4.1", body, cons)); d.Action != contract.RouteHeld || d.Reason != contract.HeldCapability {
			t.Errorf("strict target, %s: %+v", body, d)
		}
	}
}

func TestWorkspaceMapReplacesTheDefaultsWholesale(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	own := contract.RouterConfig{Mode: contract.RouterModeConservative, Map: []contract.RoutePair{{Provider: "openai", From: "gpt-4o", To: "gpt-4o-mini"}}}.Attr()
	if d := before(t, h, callAt(5, "gpt-4.1", openaiBody, own)); d.Reason != contract.HeldNotInMap {
		t.Errorf("a pair outside the workspace map must not route: %+v", d)
	}
	body := strings.Replace(openaiBody, "gpt-4.1", "gpt-4o", 1)
	if d := before(t, h, callAt(5, "gpt-4o", body, own)); d.Action != contract.RouteRouted || d.To != "gpt-4o-mini" {
		t.Errorf("the workspace pair routes: %+v", d)
	}
	empty := contract.RouterConfig{Mode: contract.RouterModeConservative, Map: []contract.RoutePair{}}.Attr()
	if d := before(t, h, callAt(5, "gpt-4.1", openaiBody, empty)); d.Reason != contract.HeldNotInMap {
		t.Errorf("an empty workspace map routes nothing: %+v", d)
	}
}

func TestThresholdFollowsTheModeAndCanBeSet(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	// a score of 5000 (an unknown tool): held by conservative, routed by aggressive
	other := []string{"frobnicate"}
	if d := before(t, h, callAt(5, "gpt-4.1", openaiBody, attr(contract.RouterModeConservative), other...)); d.Reason != router.HeldScore {
		t.Errorf("conservative at 5000: %+v", d)
	}
	if d := before(t, h, callAt(5, "gpt-4.1", openaiBody, attr(contract.RouterModeAggressive), other...)); d.Action != contract.RouteRouted {
		t.Errorf("aggressive at 5000: %+v", d)
	}
	thr := 1000
	a := contract.RouterConfig{Mode: contract.RouterModeAggressive, ThresholdBp: &thr}.Attr()
	if d := before(t, h, callAt(5, "gpt-4.1", openaiBody, a)); d.Reason != router.HeldScore {
		t.Errorf("a threshold of 1000 holds a 2500 step: %+v", d)
	}
}

func TestBodyEditFailureLeavesTheCallAlone(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	c := callAt(5, "gpt-4.1", `{"messages":[]}`, attr(contract.RouterModeConservative))
	d := before(t, h, c)
	if d.Action != contract.RouteHeld || d.Reason != router.HeldEditFailed || c.Model != "gpt-4.1" || string(c.Body) != `{"messages":[]}` {
		t.Errorf("decision %+v model %s body %s", d, c.Model, c.Body)
	}
	if h.Status()["edit_failed"] != int64(1) {
		t.Errorf("status = %v", h.Status())
	}
}

// A verdict on a routed call forces the next two calls to the strong model; the third routes again.
func TestEscalationForcesTwoStrongCalls(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	cons := attr(contract.RouterModeConservative)
	c3 := callAt(3, "gpt-4.1", openaiBody, cons)
	if d := before(t, h, c3); d.Action != contract.RouteRouted {
		t.Fatalf("call 3 must route: %+v", d)
	}
	// the monitors fire on call 3 (history's verdict pointer is the one the router kept)
	v := c3.Meta[contract.MetaMonitors].(*contract.CallVerdicts)
	v.Add(contract.MonitorVerdict{Monitor: contract.MonitorDuplicateToolCall, Mode: contract.ModeObserve, Action: contract.ModeObserve})
	for i, n := range []int{4, 5} {
		c := callAt(n, "gpt-4.1", openaiBody, cons)
		d := before(t, h, c)
		if d.Action != contract.RouteHeld || d.Reason != contract.HeldEscalated || c.Model != "gpt-4.1" || string(c.Body) != openaiBody {
			t.Fatalf("forced call %d (%d): %+v", n, i, d)
		}
	}
	if d := before(t, h, callAt(6, "gpt-4.1", openaiBody, cons)); d.Action != contract.RouteRouted {
		t.Errorf("the escalation lasts two calls: %+v", d)
	}
}

func TestNoVerdictMeansNoEscalationAndOtherSessionsAreNotAffected(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	cons := attr(contract.RouterModeConservative)
	c3 := callAt(3, "gpt-4.1", openaiBody, cons)
	before(t, h, c3)
	if d := before(t, h, callAt(4, "gpt-4.1", openaiBody, cons)); d.Action != contract.RouteRouted {
		t.Errorf("no verdict, no escalation: %+v", d)
	}
	c := callAt(4, "gpt-4.1", openaiBody, cons)
	before(t, h, c)
	c.Meta[contract.MetaMonitors].(*contract.CallVerdicts).Add(contract.MonitorVerdict{Monitor: contract.MonitorRepeatedRead})
	other := callAt(4, "gpt-4.1", openaiBody, cons)
	other.Session.ID = "other"
	if d := before(t, h, other); d.Action != contract.RouteRouted {
		t.Errorf("another session is not escalated: %+v", d)
	}
	h.OnSessionClosed(t.Context(), tollgate.Session{ID: "s1", TenantID: "ws1"})
	if n := h.Status()["sessions"]; n != 1 {
		t.Errorf("a closed session's state is dropped: %v live", n)
	}
}

func TestSessionMemoryIsBounded(t *testing.T) {
	h := router.New(router.Options{Tables: tables(), MaxSessions: 64})
	cons := attr(contract.RouterModeConservative)
	for i := range 1000 {
		c := callAt(5, "gpt-4.1", openaiBody, cons)
		c.Session.ID = "s" + string(rune('a'+i%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i/676))
		before(t, h, c)
	}
	st := h.Status()
	if n := st["sessions"].(int); n > 64 || st["evicted"].(int64) == 0 {
		t.Errorf("sessions %v evicted %v: the map must stay at its bound", st["sessions"], st["evicted"])
	}
}

// A routed call the upstream refused marks the pair unavailable for that workspace for an hour (ruling A1).
func TestUpstreamRefusalMarksThePairUnavailableForAnHour(t *testing.T) {
	clk := &clock{now: time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)}
	h := router.New(router.Options{Tables: tables(), Now: clk.Now})
	cons := attr(contract.RouterModeConservative)
	for _, status := range []int{400, 403, 404} {
		h := router.New(router.Options{Tables: tables(), Now: clk.Now})
		c := callAt(5, "gpt-4.1", openaiBody, cons)
		before(t, h, c)
		h.After(t.Context(), c, tollgate.Metric{Status: status})
		if d := before(t, h, callAt(6, "gpt-4.1", openaiBody, cons)); d.Reason != contract.HeldUnavailable || d.Action != contract.RouteHeld {
			t.Errorf("status %d: the next call must be held unavailable: %+v", status, d)
		}
	}
	c := callAt(5, "gpt-4.1", openaiBody, cons)
	before(t, h, c)
	h.After(t.Context(), c, tollgate.Metric{Status: 404})
	if st := h.Status(); st["unavailable_marked"] != int64(1) || st["unavailable_now"] != 1 {
		t.Fatalf("status = %v", st)
	}
	other := callAt(6, "gpt-4.1", openaiBody, cons)
	other.Tenant.ID = "ws2"
	if d := before(t, h, other); d.Action != contract.RouteRouted {
		t.Errorf("the mark is per workspace: %+v", d)
	}
	other2 := callAt(6, "gpt-4o", strings.Replace(openaiBody, "gpt-4.1", "gpt-4o", 1), cons)
	if d := before(t, h, other2); d.Action != contract.RouteRouted {
		t.Errorf("the mark is per pair: %+v", d)
	}
	clk.now = clk.now.Add(59 * time.Minute)
	if d := before(t, h, callAt(7, "gpt-4.1", openaiBody, cons)); d.Reason != contract.HeldUnavailable {
		t.Errorf("still unavailable at 59 minutes: %+v", d)
	}
	clk.now = clk.now.Add(2 * time.Minute)
	if d := before(t, h, callAt(8, "gpt-4.1", openaiBody, cons)); d.Action != contract.RouteRouted {
		t.Errorf("available again after an hour: %+v", d)
	}
}

func TestOnlyRoutedCallsMarkAndOnlyRefusalsCount(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	cons := attr(contract.RouterModeConservative)
	held := callAt(1, "gpt-4.1", openaiBody, cons)
	before(t, h, held)
	h.After(t.Context(), held, tollgate.Metric{Status: 404}) // a held call went to the model it asked for
	ok := callAt(5, "gpt-4.1", openaiBody, cons)
	before(t, h, ok)
	for _, s := range []int{200, 429, 500, 0} {
		h.After(t.Context(), ok, tollgate.Metric{Status: s})
	}
	obs := callAt(5, "gpt-4.1", openaiBody, attr(contract.RouterModeObserve))
	before(t, h, obs)
	h.After(t.Context(), obs, tollgate.Metric{Status: 404})
	if st := h.Status(); st["unavailable_marked"] != int64(0) {
		t.Errorf("status = %v", st)
	}
	if d := before(t, h, callAt(6, "gpt-4.1", openaiBody, cons)); d.Action != contract.RouteRouted {
		t.Errorf("nothing marked, so it routes: %+v", d)
	}
	h.After(t.Context(), callAt(5, "gpt-4.1", openaiBody, cons), tollgate.Metric{Status: 404}) // no decision in Meta: ignored
}

func TestConfigMemoSurvivesManyCallsAndBadAttrs(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	for range 3 {
		c := callAt(5, "gpt-4.1", openaiBody, `{"mode":"yolo"}`)
		_, _ = h.Before(t.Context(), c)
		if _, stamped := c.Meta[contract.MetaRouter]; stamped {
			t.Fatal("an unknown mode resets to off")
		}
	}
}

func TestStatusCountsDecisions(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	cons := attr(contract.RouterModeConservative)
	before(t, h, callAt(1, "gpt-4.1", openaiBody, cons))
	before(t, h, callAt(5, "gpt-4.1", openaiBody, cons))
	before(t, h, callAt(5, "gpt-4.1", openaiBody, attr(contract.RouterModeObserve)))
	st := h.Status()
	by := st["held_by_reason"].(map[string]any)
	if st["routed"] != int64(1) || st["observed"] != int64(1) || st["held"] != int64(1) || by[contract.HeldFirstSteps] != int64(1) {
		t.Errorf("status = %v", st)
	}
	var _ contract.StatusSource = h
}

func TestBodyOtherThanModelIsByteIdentical(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	body := "{ \"messages\" : [ {\"role\":\"user\",\"content\":\"a \\\"model\\\": \\\"x\\\" b\"} ] ,\n \"model\"  :  \"gpt-4.1\" , \"temperature\":0.2 }"
	c := callAt(5, "gpt-4.1", body, attr(contract.RouterModeConservative))
	before(t, h, c)
	want := strings.Replace(body, `"gpt-4.1"`, `"gpt-4.1-mini"`, 1)
	if !bytes.Equal(c.Body, []byte(want)) {
		t.Errorf("got  %q\nwant %q", c.Body, want)
	}
}
