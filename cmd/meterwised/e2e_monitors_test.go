package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
)

// Plan 06 end to end: the real serve wiring (wsauth, history, monitors, ledger) in front of a loopback
// fake upstream, driven through /w/{id} and read back through the control plane. One agent asks for
// the same weather again and again; the workspace's duplicate_tool_call mode decides what happens.

type wireCase struct {
	name, path, model string
	nonStream, stream string // upstream fixture per stream mode
	anthropic         bool
}

var monitorWires = []wireCase{
	{"openai", "/openai/v1/chat/completions", "gpt-4o", "openai/chat", "openai/chat-stream-tools", false},
	{"anthropic", "/anthropic/v1/messages", "claude-sonnet-4-5", "anthropic/messages", "anthropic/messages-stream-tool-use", true},
}

// weather is an agent that asked for the same weather n times: n assistant turns with the same tool call
// and the same result, the last result pending (the next call's request). It is rendered in w's JSON.
func (w wireCase) weather(n int, stream bool) []byte {
	msgs := []map[string]any{{"role": "user", "content": "what is the weather in Paris"}}
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("call_%d", i)
		if w.anthropic {
			msgs = append(msgs,
				map[string]any{"role": "assistant", "content": []map[string]any{{"type": "tool_use", "id": id, "name": "get_weather", "input": map[string]any{"city": "Paris"}}}},
				map[string]any{"role": "user", "content": []map[string]any{{"type": "tool_result", "tool_use_id": id, "content": "sunny, 21C"}}})
			continue
		}
		msgs = append(msgs,
			map[string]any{"role": "assistant", "content": "", "tool_calls": []map[string]any{{"id": id, "type": "function", "function": map[string]any{"name": "get_weather", "arguments": `{"city":"Paris"}`}}}},
			map[string]any{"role": "tool", "tool_call_id": id, "content": "sunny, 21C"})
	}
	doc := map[string]any{"model": w.model, "stream": stream, "messages": msgs}
	if w.anthropic {
		doc["max_tokens"] = 1024
		doc["system"] = "You are a test agent."
	}
	b, _ := json.Marshal(doc)
	return b
}

// agent is one client of one workspace on one loopback upstream.
type agent struct {
	t  *testing.T
	p  *plane
	up *testkit.FakeUpstream
}

type sent struct {
	status   int
	header   http.Header
	body     []byte // what the client got
	request  []byte // what the client sent
	upstream []byte // what the upstream received for it; nil when it never got there
}

// call sends the n-turn weather request of session on w and reports what the client got and what the
// upstream saw.
func (a *agent) call(w wireCase, session string, n int, stream bool) sent {
	a.t.Helper()
	body := w.weather(n, stream)
	fixture := w.nonStream
	if stream {
		fixture = w.stream
	}
	before := len(a.up.Requests())
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, a.p.proxy+"/w/"+a.p.ws.PublicID+w.path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-caller-own-key")
	req.Header.Set(testkit.FixtureHeader, fixture)
	req.Header.Set("x-meterwise-session", session)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	s := sent{status: resp.StatusCode, header: resp.Header, body: got, request: body}
	if reqs := a.up.Requests(); len(reqs) > before {
		s.upstream = reqs[len(reqs)-1].Body
	}
	return s
}

func (a *agent) send(method, path string, body any) (int, []byte) {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, a.p.api+path, rd)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// setMode PATCHes duplicate_tool_call to mode; the next call must see it with no restart.
func (a *agent) setMode(mode contract.MonitorMode) {
	a.t.Helper()
	code, b := a.send(http.MethodPatch, "/api/workspaces/"+a.p.ws.ID+"/monitors", map[string]any{"duplicate_tool_call": map[string]any{"mode": mode}})
	if code != 200 {
		a.t.Fatalf("PATCH monitors %s: %d %s", mode, code, b)
	}
	var cfg contract.MonitorConfig
	if err := json.Unmarshal(b, &cfg); err != nil || cfg.DuplicateToolCall.Mode != mode {
		a.t.Fatalf("PATCH answered %s (%v)", b, err)
	}
}

// detail waits until the session has n calls recorded and returns it.
func (a *agent) detail(session string, n int) contract.SessionDetail {
	a.t.Helper()
	var d contract.SessionDetail
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		code, b := a.send(http.MethodGet, "/api/sessions/"+url.PathEscape(session)+"?workspaceId="+a.p.ws.ID, nil)
		if code == 200 && json.Unmarshal(b, &d) == nil && len(d.Calls) >= n {
			return d
		}
	}
	a.t.Fatalf("session %s: %d of %d calls recorded", session, len(d.Calls), n)
	return d
}

func proxyNotes(b []byte) int { return bytes.Count(b, []byte("[proxy]")) }

func monitorBootAgent(t *testing.T) *agent {
	t.Helper()
	up := testkit.NewFakeUpstream(t, testkit.Options{})
	base, _ := url.Parse(up.URL)
	return &agent{t: t, p: bootPlane(t, dataplane.Deps{Resolver: bothWires{base}}, nil), up: up}
}

func TestMonitorsObserveLeavesTheUpstreamBodyByteIdenticalAndOnlyWouldSaveMoves(t *testing.T) {
	a := monitorBootAgent(t)
	for _, w := range monitorWires {
		session := "obs-" + w.name
		for n := 0; n < 3; n++ {
			s := a.call(w, session, n, false)
			if s.status != 200 || !bytes.Equal(s.upstream, s.request) {
				t.Fatalf("%s call %d: status %d, upstream body changed under observe", w.name, n+1, s.status)
			}
		}
		d := a.detail(session, 3)
		if len(d.MonitorEvents) == 0 {
			t.Fatalf("%s: observe recorded no event", w.name)
		}
		for _, ev := range d.MonitorEvents {
			if ev.Action != contract.ModeObserve || ev.Monitor != contract.MonitorDuplicateToolCall {
				t.Fatalf("%s: event %+v", w.name, ev)
			}
		}
		if d.Session.SavedUsd != 0 || d.Session.LikelySavedUsd != 0 {
			t.Fatalf("%s: observe moved a savings number: saved %v likely %v", w.name, d.Session.SavedUsd, d.Session.LikelySavedUsd)
		}
		for _, c := range d.Calls {
			if c.Blocked || c.NotesInjected != 0 {
				t.Fatalf("%s: observe touched call %+v", w.name, c)
			}
		}
	}
	var st contract.MonitorStatsView
	a.p.getJSON(t, "/api/workspaces/"+a.p.ws.ID+"/monitors/stats?range=7d", &st)
	if st.SavedUsd != 0 || st.LikelySavedUsd != 0 || st.WouldSaveUsd <= 0 {
		t.Fatalf("stats after observe: saved %v likely %v would-save %v", st.SavedUsd, st.LikelySavedUsd, st.WouldSaveUsd)
	}
}

func TestMonitorsNoteAddsExactlyOneProxyNoteAndPATCHSwitchesModeWithoutARestart(t *testing.T) {
	a := monitorBootAgent(t)
	for _, w := range monitorWires {
		session := "note-" + w.name
		// the default is observe: the first calls are untouched; PATCH to note between two calls of one run
		for n := 0; n < 2; n++ {
			if s := a.call(w, session, n, false); !bytes.Equal(s.upstream, s.request) {
				t.Fatalf("%s call %d changed before any PATCH", w.name, n+1)
			}
		}
		a.setMode(contract.ModeNote)
		s := a.call(w, session, 2, false)
		if s.status != 200 || proxyNotes(s.upstream) != 1 || !json.Valid(s.upstream) || proxyNotes(s.request) != 0 {
			t.Fatalf("%s: want exactly one [proxy] note in the upstream body after PATCH: %d notes, status %d\n%s", w.name, proxyNotes(s.upstream), s.status, s.upstream)
		}
		d := a.detail(session, 3)
		if d.Calls[2].NotesInjected != 1 || d.Calls[2].Blocked {
			t.Fatalf("%s: call 3 %+v", w.name, d.Calls[2])
		}
		last := d.MonitorEvents[len(d.MonitorEvents)-1]
		if last.Action != contract.ModeNote || last.Mode != contract.ModeNote {
			t.Fatalf("%s: event %+v", w.name, last)
		}
		a.setMode(contract.ModeObserve)
	}
	// every PATCH left an audit row naming the monitor and its new mode
	var audit contract.AuditList
	a.p.getJSON(t, "/api/audit-log?action=workspace.monitors_update&workspaceId="+a.p.ws.ID, &audit)
	if len(audit.Items) != 4 {
		t.Fatalf("audit rows for 4 PATCHes: %d", len(audit.Items))
	}
	modes := map[string]int{}
	for _, it := range audit.Items {
		m, _ := it.Metadata["modes"].(map[string]any)
		mode, _ := m[string(contract.MonitorDuplicateToolCall)].(string)
		modes[mode]++
	}
	if modes["note"] != 2 || modes["observe"] != 2 {
		t.Fatalf("audit metadata modes %v", modes)
	}
}

// The agent repeats itself; the third call (the first repeat) is blocked: the upstream is not called and the
// client gets a terminal text answer in its own wire. A fourth call that repeats the same subject is noted and
// reaches the upstream: one block per session and subject.
func TestMonitorsBlockAnswersWithoutTheUpstreamOnBothWiresAndFeedbackAndReconcile(t *testing.T) {
	a := monitorBootAgent(t)
	a.setMode(contract.ModeBlock)
	var blockedEvents []contract.MonitorEventView
	sessions := map[string]bool{}
	for _, w := range monitorWires {
		for _, stream := range []bool{false, true} {
			session := fmt.Sprintf("blk-%s-%v", w.name, stream)
			sessions[session] = true
			var prev sent
			for n := 0; n < 2; n++ {
				if prev = a.call(w, session, n, stream); prev.status != 200 {
					t.Fatalf("%s call %d: %d %s", session, n+1, prev.status, prev.body)
				}
			}
			before := len(a.up.Requests())
			s := a.call(w, session, 2, stream)
			if got := len(a.up.Requests()); got != before || s.upstream != nil {
				t.Fatalf("%s: a blocked call reached the upstream (%d -> %d requests)", session, before, got)
			}
			if again := a.call(w, session, 3, stream); again.status != 200 || len(a.up.Requests()) != before+1 {
				t.Fatalf("%s: the second repeat must be noted and reach the upstream: %d", session, again.status)
			}
			assertWholeAnswer(t, session, stream, s)
			d := a.detail(session, 4)
			if !d.Calls[2].Blocked || d.Calls[2].InputTokens != 0 || d.Calls[2].OutputTokens != 0 || d.Calls[3].Blocked || d.Calls[3].NotesInjected != 1 {
				t.Fatalf("%s: call 3 %+v, call 4 %+v", session, d.Calls[2], d.Calls[3])
			}
			var certain *contract.MonitorEventView
			for i, ev := range d.MonitorEvents {
				if ev.Action == contract.ModeBlock {
					certain = &d.MonitorEvents[i]
				}
			}
			if certain == nil || certain.Confidence != contract.ConfidenceCertain || certain.CallIndex != 3 || certain.EstSavedTokens <= 0 || certain.EstSavedUsd <= 0 {
				t.Fatalf("%s: no certain block event: %+v", session, d.MonitorEvents)
			}
			if d.Session.SavedUsd <= 0 {
				t.Fatalf("%s: session saved_usd did not move: %+v", session, d.Session)
			}
			if want := blockedEstimate(d.Calls[:2], len(s.request)-len(prev.request)); certain.EstSavedTokens != want {
				t.Fatalf("%s: blocked estimate %d tokens, want %d (prior input + ceil(body delta / 4) + median output)", session, certain.EstSavedTokens, want)
			}
			blockedEvents = append(blockedEvents, *certain)
		}
	}

	// feedback: one correct, one wrong, a third left alone -> false-positive rate 1/2
	fb := func(ev contract.MonitorEventView, v string) {
		code, b := a.send(http.MethodPost, "/api/workspaces/"+a.p.ws.ID+"/monitor-events/"+ev.ID+"/feedback", map[string]any{"feedback": v})
		var got contract.MonitorEventView
		if code != 200 || json.Unmarshal(b, &got) != nil || got.Feedback == nil || *got.Feedback != v {
			t.Fatalf("feedback %s: %d %s", v, code, b)
		}
	}
	fb(blockedEvents[0], "correct")
	fb(blockedEvents[1], "wrong")
	var st contract.MonitorStatsView
	a.p.getJSON(t, "/api/workspaces/"+a.p.ws.ID+"/monitors/stats?range=7d", &st)
	dup := st.Monitors[0]
	if dup.Monitor != contract.MonitorDuplicateToolCall || dup.FeedbackCorrect != 1 || dup.FeedbackWrong != 1 || dup.FalsePositiveRate == nil || *dup.FalsePositiveRate != 0.5 {
		t.Fatalf("stats after feedback: %+v", dup)
	}
	if dup.Blocked != 4 || dup.Mode != contract.ModeBlock {
		t.Fatalf("stats row: %+v", dup)
	}
	if code, _ := a.send(http.MethodPost, "/api/workspaces/"+a.p.ws.ID+"/monitor-events/999999999/feedback", map[string]any{"feedback": "wrong"}); code != 404 {
		t.Fatalf("unknown event: %d", code)
	}
	a.reconcile(t, st)
}

// assertWholeAnswer checks a blocked call got a valid answer in its own wire and stream mode.
func assertWholeAnswer(t *testing.T, name string, stream bool, s sent) {
	t.Helper()
	ct, body := s.header.Get("Content-Type"), string(s.body)
	if s.status != 200 || proxyNotes(s.body) != 1 || !strings.Contains(body, "get_weather") {
		t.Fatalf("%s: block answer %d: %s", name, s.status, body)
	}
	if strings.Contains(body, "tool_calls") || strings.Contains(body, "tool_use") {
		t.Fatalf("%s: a block is a terminal text turn and names no tool: %s", name, body)
	}
	if !stream {
		var doc map[string]any
		if !strings.HasPrefix(ct, "application/json") || json.Unmarshal(s.body, &doc) != nil {
			t.Fatalf("%s: want one JSON document, got %q: %s", name, ct, body)
		}
		return
	}
	if !strings.HasPrefix(ct, "text/event-stream") || !strings.HasSuffix(body, "\n\n") {
		t.Fatalf("%s: want whole SSE frames, got %q", name, ct)
	}
	for _, f := range strings.Split(strings.TrimSuffix(body, "\n\n"), "\n\n") {
		i := strings.Index(f, "data: ")
		var x any
		if i < 0 || (f[i+6:] != "[DONE]" && json.Unmarshal([]byte(f[i+6:]), &x) != nil) {
			t.Fatalf("%s: bad SSE frame %q", name, f)
		}
	}
}

// reconcile: the two savings numbers on the sessions and in the stats equal SUM(monitor_events) split by
// confidence (observe rows excluded, they are would-save), and no field is their sum.
func (a *agent) reconcile(t *testing.T, st contract.MonitorStatsView) {
	t.Helper()
	var evSaved, evLikely, evWould, sesSaved, sesLikely string
	err := a.p.rt.db.Pool().QueryRow(context.Background(), `
		SELECT COALESCE(SUM(est_saved_usd) FILTER (WHERE confidence='certain' AND action<>'observe'),0)::text,
		       COALESCE(SUM(est_saved_usd) FILTER (WHERE confidence='likely' AND action<>'observe'),0)::text,
		       COALESCE(SUM(est_saved_usd) FILTER (WHERE action='observe'),0)::text,
		       (SELECT COALESCE(SUM(saved_usd),0)::text FROM sessions WHERE workspace_id=$1),
		       (SELECT COALESCE(SUM(likely_saved_usd),0)::text FROM sessions WHERE workspace_id=$1)
		FROM monitor_events WHERE workspace_id=$1`, a.p.ws.ID).Scan(&evSaved, &evLikely, &evWould, &sesSaved, &sesLikely)
	if err != nil {
		t.Fatal(err)
	}
	money := func(label, db string, api contract.MicroUSD) {
		want, perr := contract.ParseMicroUSD(db)
		if perr != nil || want != api {
			t.Fatalf("%s: SUM in the database %s (%v), the API says %v", label, db, perr, api)
		}
	}
	money("events certain vs stats saved", evSaved, st.SavedUsd)
	money("events likely vs stats likely", evLikely, st.LikelySavedUsd)
	money("events observe vs stats would-save", evWould, st.WouldSaveUsd)
	money("events certain vs sessions saved_usd", evSaved, mustMoney(t, sesSaved))
	money("events likely vs sessions likely_saved_usd", evLikely, mustMoney(t, sesLikely))
	var raw map[string]any
	a.p.getJSON(t, "/api/workspaces/"+a.p.ws.ID+"/monitors/stats?range=7d", &raw)
	for k := range raw {
		if strings.Contains(strings.ToLower(k), "total") {
			t.Fatalf("stats has a summed field %q", k)
		}
	}
	if st.SavedUsd <= 0 {
		t.Fatalf("a block run must have saved something: %+v", st)
	}
	var stats map[string]any
	a.p.getJSON(t, "/api/workspaces/"+a.p.ws.ID+"/stats?range=7d", &stats)
	if _, ok := stats["savedUsd"].(string); !ok {
		t.Fatalf("workspace stats lacks savedUsd: %v", stats)
	}
	if _, ok := stats["likelySavedUsd"].(string); !ok {
		t.Fatalf("workspace stats lacks likelySavedUsd: %v", stats)
	}
}

func mustMoney(t *testing.T, s string) contract.MicroUSD {
	t.Helper()
	m, err := contract.ParseMicroUSD(s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// blockedEstimate is the certain estimate of a blocked call, from the ledger's rows of the calls before
// it: the previous call's input (cached included) + ceil(body growth since / 4) + the median output.
func blockedEstimate(prior []contract.CallView, delta int) int64 {
	last := prior[len(prior)-1]
	outs := make([]int64, 0, len(prior))
	for _, c := range prior {
		if c.OutputTokens > 0 {
			outs = append(outs, c.OutputTokens)
		}
	}
	slices.Sort(outs)
	var median int64
	if len(outs) > 0 {
		median = outs[len(outs)/2]
	}
	return last.InputTokens + last.CachedTokens + (int64(max(0, delta))+3)/4 + median
}
