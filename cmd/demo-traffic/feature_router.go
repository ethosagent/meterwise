package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/router"
	"github.com/ethosagent/meterwise/internal/store"
)

// routerChain fills the router tables of the demo's hook chain, priced from the same table the ledger uses, the way
// serve does (router.BuildTables over the seeded route and capability rows). The hook only acts on a workspace whose
// router mode is not off, so loading the tables for every run changes nothing for the default flags.
func routerChain(ctx context.Context, st *store.Store, pricer contract.Pricer, chain *dataplane.ChainOptions, out io.Writer) error {
	routes, caps, err := st.RouterRows(ctx)
	if err != nil {
		return err
	}
	tables, dropped := router.BuildTables(routes, caps, pricer, time.Now())
	chain.Router = tables
	// Like serve, say which default pairs were left out and why, instead of silently routing fewer.
	for _, d := range dropped {
		fmt.Fprintf(out, "router pair dropped: %s %s -> %s (%s)\n", d.Pair.Provider, d.Pair.From, d.Pair.To, d.Reason)
	}
	return nil
}

// routerScenarioPrefix marks the requests the fake upstream answers by echoing the model it was asked for, so the
// router's choice (and a refused target) is visible in the ledger. The request body, not a header, says the model.
const routerScenarioPrefix = "router-"

// routerUpstream answers a router scenario request: 404 for the model the demo marks as unavailable (gpt-4o-mini),
// otherwise a short text answer from whatever model the proxy sent. It reports false for any other request.
func routerUpstream(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.Header.Get(scenarioHeader), routerScenarioPrefix) {
		return false
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var in struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(raw, &in)
	n, _ := strconv.Atoi(r.Header.Get(weightHeader))
	inTok, outTok := 1000+n*7, 100+n*3
	w.Header().Set("Content-Type", "application/json")
	if in.Model == routerRefusedModel {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"The model does not exist or you do not have access to it.","type":"invalid_request_error","param":null,"code":"model_not_found"}}`))
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/anthropic/") {
		_, _ = fmt.Fprintf(w, `{"id":"msg_demo","type":"message","role":"assistant","model":%q,"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":%d,"output_tokens":%d}}`, in.Model, inTok, outTok)
		return true
	}
	_, _ = fmt.Fprintf(w, `{"id":"chatcmpl-demo","object":"chat.completion","created":1762500000,"model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d}}`, in.Model, inTok, outTok, inTok+outTok)
	return true
}

// routerRefusedModel is the cheaper model the fake upstream refuses, to show a target the provider does not serve.
const routerRefusedModel = "gpt-4o-mini"

// routerRun is one scripted agent session of the router scenarios: it reads files turn after turn (the cheap-step
// shape the scorer looks for), on a model the default map has a cheaper sibling for.
type routerRun struct {
	name   string
	wire   wireShape
	model  string
	turns  int                   // requests sent: turn t carries t finished tool calls
	result func(turn int) string // the tool result of finished call turn
	extra  map[string]any        // fields added to every request (tools, output_format)
	tool   string                // tool the agent keeps calling; read_file by default
	args   func(turn int) string // its arguments; distinct per turn so no monitor sees a repeat
}

func readResult(turn int) string {
	return fmt.Sprintf("// file %d\npackage demo%d\n\nfunc F%d() int { return %d }", turn, turn, turn, turn*7)
}

func errorResult(last int) func(int) string {
	return func(turn int) string {
		if turn == last {
			return "Traceback (most recent call last):\n  File \"main.py\", line 3, in <module>\nNameError: name 'x' is not defined"
		}
		return readResult(turn)
	}
}

var oa = wireShapes[0]
var an = wireShapes[1]

var routerRuns = []routerRun{
	// Read-heavy: calls 1 and 2 stay on the strong model (first steps); from call 3 each read turn scores simple.
	{name: "reads", wire: oa, model: "gpt-4.1", turns: 6, result: readResult},
	// The same, asking for a dated id: it matches the gpt-4.1 pair.
	{name: "dated", wire: oa, model: "gpt-4.1-2025-04-14", turns: 4, result: readResult},
	// The last tool result is a traceback: that call is held as an error retry.
	{name: "error-retry", wire: oa, model: "gpt-4.1", turns: 5, result: errorResult(3)},
	// Tools toward a target that takes none (claude-haiku-4-5): held by capability, never routed.
	{name: "capability", wire: an, model: "claude-opus-4-5", turns: 4, result: readResult,
		extra: map[string]any{"tools": []map[string]any{{"name": "read_file", "description": "read a file", "input_schema": map[string]any{"type": "object"}}}}},
	// The target is refused by the provider: the first routed call is marked unavailable and the rest are held.
	{name: "refused", wire: oa, model: "gpt-4o", turns: 5, result: readResult},
}

// body renders turn t of the run through the same client code the looping agents use.
func (r routerRun) body(turn int) ([]byte, error) {
	w := r.wire
	w.model = r.model
	tool, args := r.tool, r.args
	if tool == "" {
		tool = "read_file"
	}
	if args == nil {
		args = func(t int) string { return fmt.Sprintf(`{"path":"src/file%d.go"}`, t) }
	}
	raw, err := loopKind{prompt: "Summarise how the demo package fits together.", tool: tool, args: args, result: r.result}.body(w, false, turn)
	if err != nil || len(r.extra) == 0 {
		return raw, err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	for k, v := range r.extra {
		doc[k] = v
	}
	return json.Marshal(doc)
}

// routerRounds plays the router scenarios after the base rounds, when the flag turned the router on.
func routerRounds(ctx context.Context, c *client, cfg config) error {
	if cfg.router == "" || cfg.router == string(contract.RouterModeOff) {
		return nil
	}
	for _, run := range routerRuns {
		session := fmt.Sprintf("%s-router-%s", sessionID(c.stamp, 0), run.name)
		c.sessions = append(c.sessions, session)
		for turn := 0; turn < run.turns; turn++ {
			body, err := run.body(turn)
			if err != nil {
				return err
			}
			if _, err := c.post(ctx, run.wire.path, string(body), session, map[string]string{scenarioHeader: routerScenarioPrefix + run.name}); err != nil {
				return err
			}
		}
	}
	return nil
}
