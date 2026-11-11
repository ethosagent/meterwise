package dataplane_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ethosagent/tollgate"
	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/monitors"
	"github.com/ethosagent/meterwise/internal/trim"
	"github.com/ethosagent/meterwise/internal/wire"
)

// trimRig is the real hook chain (history, trim, monitors, a ledger stand-in) in front of testkit's FakeUpstream.
type trimRig struct {
	t     *testing.T
	up    *testkit.FakeUpstream
	srv   *httptest.Server
	hooks []tollgate.Hook
	seen  chan *tollgate.Call
}

type tenantWith struct{ attrs map[string]string }

func (a tenantWith) Authenticate(context.Context, *http.Request) (tollgate.Tenant, error) {
	return tollgate.Tenant{ID: "ws", Attrs: a.attrs}, nil
}

// tapHook stands in for the ledger: it hands the call's Meta to the test once the call is done.
type tapHook struct {
	tollgate.NoopHook
	seen chan *tollgate.Call
}

func (*tapHook) Name() string { return "ledger" }
func (h *tapHook) After(_ context.Context, c *tollgate.Call, _ tollgate.Metric) {
	h.seen <- c
}

func newTrimRig(t *testing.T, mode contract.TrimMode, dupMode contract.MonitorMode) *trimRig {
	t.Helper()
	tc := contract.DefaultTrimConfig()
	tc.Mode = mode
	mc := contract.DefaultMonitorConfig()
	mc.DuplicateToolCall.Mode = dupMode
	attrs := map[string]string{trim.AttrTrim: tc.Attr(), monitors.AttrMonitors: mc.Attr()}
	r := &trimRig{t: t, seen: make(chan *tollgate.Call, 64)}
	r.up = testkit.NewFakeUpstream(t, testkit.Options{})
	base, _ := url.Parse(r.up.URL)
	provs := map[string]tollgate.Provider{
		"openai":    {Name: "openai", Wire: tollgate.WireOpenAIChat, BaseURL: base},
		"anthropic": {Name: "anthropic", Wire: tollgate.WireAnthropicMessages, BaseURL: base},
	}
	hooks, sources := dataplane.HookChain(dataplane.ChainOptions{PrivacyMode: true}, nil, &tapHook{seen: r.seen})
	r.hooks = hooks
	if len(sources) < 3 {
		t.Fatalf("history, trim and monitors report status: %d sources", len(sources))
	}
	p, err := dataplane.New(cfg(t, map[string]string{}), dataplane.Deps{Auth: tenantWith{attrs}, Resolver: dataplane.NewWorkspaceResolver(provs, true), Hooks: hooks})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.srv = httptest.NewServer(p.Handler())
	t.Cleanup(func() { r.srv.Close(); _ = p.Close(context.Background()) })
	return r
}

type tstep struct{ args, result string }

// body renders a request carrying the steps; "messages" is last, so a request's bytes up to the end of its messages are
// a prefix of the next request's.
func body(t *testing.T, w tollgate.WireFormat, steps []tstep) []byte {
	t.Helper()
	var msgs []any
	var head string
	if w == tollgate.WireOpenAIChat {
		head = `{"model":"gpt-4.1","stream":false,"tools":[{"type":"function","function":{"name":"get_build_log","description":"Run a command"}}],`
		msgs = []any{map[string]any{"role": "system", "content": "You are an agent."}, map[string]any{"role": "user", "content": "fix the build"}}
		for i, s := range steps {
			id := fmt.Sprintf("call_%d", i)
			msgs = append(msgs,
				map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "get_build_log", "arguments": s.args}}}},
				map[string]any{"role": "tool", "tool_call_id": id, "content": s.result})
		}
	} else {
		head = `{"model":"claude-sonnet-4-5","max_tokens":64,"system":[{"type":"text","text":"You are an agent.","cache_control":{"type":"ephemeral"}}],"tools":[{"name":"get_build_log","description":"Run a command","input_schema":{"type":"object"}}],`
		msgs = []any{map[string]any{"role": "user", "content": "fix the build"}}
		for i, s := range steps {
			id := fmt.Sprintf("tu_%d", i)
			blk := map[string]any{"type": "tool_result", "tool_use_id": id, "content": s.result}
			if i == 1 { // a cache breakpoint on the second result
				blk["cache_control"] = map[string]any{"type": "ephemeral"}
			}
			msgs = append(msgs,
				map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": id, "name": "get_build_log", "input": json.RawMessage(s.args)}}},
				map[string]any{"role": "user", "content": []any{blk}})
		}
	}
	m, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(head+`"messages":`), append(m, '}')...)
}

func buildLog() string {
	var b strings.Builder
	for i := 1; i <= 300; i++ {
		if i == 150 {
			b.WriteString("src/mod_150.c:12:5: error: expected ';' before '}' token\n")
			continue
		}
		fmt.Fprintf(&b, "[%d/300] CC src/mod_%d.c\n", i, i)
	}
	return b.String()
}

func (r *trimRig) post(w tollgate.WireFormat, b []byte) (int, *tollgate.Call) {
	r.t.Helper()
	path, fixture := "/openai/v1/chat/completions", "openai/chat"
	if w == tollgate.WireAnthropicMessages {
		path, fixture = "/anthropic/v1/messages", "anthropic/messages"
	}
	req, _ := http.NewRequest(http.MethodPost, r.srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-session-id", "trim-session")
	req.Header.Set(testkit.FixtureHeader, fixture)
	resp, err := r.srv.Client().Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, <-r.seen
}

func results(t *testing.T, w tollgate.WireFormat, b []byte) []string {
	t.Helper()
	rs, err := wire.ToolResults(w, b)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(rs))
	for i, g := range rs {
		out[i] = g.Text
	}
	return out
}

// T3 through the real chain: in a 6-call session the bytes the upstream receives for the shared messages equal the
// previous call's, on both wires. Call k carries the k-1 tool results of the calls before it, as an agent's does.
func TestChainSendsByteIdenticalPrefixesToTheUpstream(t *testing.T) {
	for _, w := range []tollgate.WireFormat{tollgate.WireOpenAIChat, tollgate.WireAnthropicMessages} {
		t.Run(string(w), func(t *testing.T) {
			r := newTrimRig(t, contract.TrimModeLossless, contract.ModeObserve)
			var steps []tstep
			for i := 0; i < 5; i++ {
				steps = append(steps, tstep{args: fmt.Sprintf(`{"cmd":"make step%d"}`, i), result: strings.ReplaceAll(buildLog(), "mod_", fmt.Sprintf("m%d_", i))})
			}
			for call := 1; call <= 6; call++ {
				if status, _ := r.post(w, body(t, w, steps[:call-1])); status != http.StatusOK {
					t.Fatalf("call %d: status %d", call, status)
				}
			}
			ups := r.up.Requests()
			if len(ups) != 6 {
				t.Fatalf("upstream saw %d requests, want 6", len(ups))
			}
			for i := 1; i < len(ups); i++ {
				prev := ups[i-1].Body
				if !bytes.HasPrefix(ups[i].Body, prev[:len(prev)-2]) {
					t.Errorf("call %d: the upstream's bytes for the shared messages changed", i+1)
				}
			}
			for i, u := range ups[1:] {
				if orig := body(t, w, steps[:i+1]); len(u.Body) >= len(orig)/2 {
					t.Errorf("call %d: upstream body %d bytes of %d: trimmed too little", i+2, len(u.Body), len(orig))
				}
			}
		})
	}
}

// T5 through the real chain, with duplicate_tool_call in block mode: when the agent repeats a call whose result was
// folded, the monitors do not block it the first time and the upstream receives the earlier result unfolded.
func TestRepeatedCallAfterAFoldIsNotBlockedAndIsSentUnfolded(t *testing.T) {
	for _, w := range []tollgate.WireFormat{tollgate.WireOpenAIChat, tollgate.WireAnthropicMessages} {
		t.Run(string(w), func(t *testing.T) {
			r := newTrimRig(t, contract.TrimModeLossless, contract.ModeBlock)
			mk := tstep{args: `{"cmd":"make all"}`, result: buildLog()}
			r.post(w, body(t, w, nil))
			_, c := r.post(w, body(t, w, []tstep{mk}))
			if _, ok := c.Meta[contract.MetaTrim].(contract.TrimResult); !ok {
				t.Fatal("call 2 was not trimmed")
			}
			if got := results(t, w, r.up.Requests()[1].Body); !strings.Contains(got[0], "lines folded") {
				t.Fatal("the first result must reach the upstream folded")
			}
			// the agent asks for the same command again
			status, c := r.post(w, body(t, w, []tstep{mk, mk}))
			ups := r.up.Requests()
			if status != 200 || len(ups) != 3 {
				t.Fatalf("call 3 was blocked or failed: status %d, upstream saw %d requests", status, len(ups))
			}
			got := results(t, w, ups[2].Body)
			if got[0] != mk.result || got[1] != mk.result {
				t.Errorf("the repeat must be sent unfolded, got %q / %q", clip(got[0]), clip(got[1]))
			}
			if _, ok := c.Meta[contract.MetaTrimUnfold].(contract.TrimUnfold); !ok {
				t.Error("Meta[trim.unfold] was not set")
			}
			if vs, ok := c.Meta[contract.MetaMonitors].(*contract.CallVerdicts); !ok || len(vs.Snapshot()) != 0 {
				t.Errorf("the monitors fired on the excused repeat")
			}
			// a third identical call is a real repeat: the monitors act on it, there is no excuse
			r.post(w, body(t, w, []tstep{mk, mk, mk}))
			if n := len(r.up.Requests()); n != 3 {
				t.Errorf("the third identical call should be blocked before the upstream, which saw %d requests", n)
			}
		})
	}
}

// Control: with trim off the same repeat is blocked by the monitors on its second occurrence, so the excuse above is
// the trim hook's doing.
func TestWithoutTrimTheSameRepeatIsBlocked(t *testing.T) {
	r := newTrimRig(t, contract.TrimModeOff, contract.ModeBlock)
	mk := tstep{args: `{"cmd":"make all"}`, result: buildLog()}
	r.post(tollgate.WireOpenAIChat, body(t, tollgate.WireOpenAIChat, nil))
	r.post(tollgate.WireOpenAIChat, body(t, tollgate.WireOpenAIChat, []tstep{mk}))
	r.post(tollgate.WireOpenAIChat, body(t, tollgate.WireOpenAIChat, []tstep{mk, mk}))
	if n := len(r.up.Requests()); n != 2 {
		t.Errorf("with trim off the repeat is blocked before the upstream: %d requests", n)
	}
}

func clip(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}

func TestTrimIsInTheDefaultChainAndReportsStatus(t *testing.T) {
	hooks, sources := dataplane.HookChain(dataplane.ChainOptions{}, nil, &tapHook{seen: make(chan *tollgate.Call, 1)})
	var names []string
	for _, h := range hooks {
		names = append(names, h.Name())
	}
	if got, want := strings.Join(names, ","), "wsauth,history,trim,monitors,ledger"; got != want {
		t.Errorf("chain = %s, want %s", got, want)
	}
	found := false
	for _, s := range sources {
		if _, ok := s.Status()["memo_evictions"]; ok {
			found = true
		}
	}
	if !found {
		t.Error("the trim counters are not a status source")
	}
}
