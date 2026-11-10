package monitors_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/monitors"
)

var bg = context.Background()

func TestObserveRecordsAndNeverTouchesTheCall(t *testing.T) {
	for _, w := range wires {
		t.Run(string(w), func(t *testing.T) {
			h := newMon(monitors.Options{})
			c := before(t, w, dupRing(), nil, 4, weatherTranscript(3))
			orig := append([]byte(nil), c.Body...)
			resp, err := h.Before(bg, c)
			if resp != nil || err != nil || string(c.Body) != string(orig) {
				t.Fatalf("observe must not change the call: %v %v", resp, err)
			}
			vs, notes := verdictsOf(c)
			if len(vs) != 1 || notes != 0 {
				t.Fatalf("verdicts %+v notes %d", vs, notes)
			}
			v := vs[0]
			wantBasis := contract.EstBasis{PriorInputTokens: 200, BodyDeltaBytes: int64(len(orig) - 100), MedianOutputTokens: 20}
			if v.Monitor != contract.MonitorDuplicateToolCall || v.Mode != contract.ModeObserve || v.Action != contract.ModeObserve || v.Basis != wantBasis {
				t.Errorf("verdict %+v, want basis %+v", v, wantBasis)
			}
			if v.Evidence.Count != 2 || !reflect.DeepEqual(v.Evidence.MatchedCalls, []int{1, 3}) || v.Evidence.Tool != "get_weather" {
				t.Errorf("evidence %+v", v.Evidence)
			}
		})
	}
}

func TestNoteModeAddsOneNoteAndDedupesPerSession(t *testing.T) {
	for _, w := range wires {
		t.Run(string(w), func(t *testing.T) {
			h := newMon(monitors.Options{})
			c := before(t, w, dupRing(), dupMode(contract.ModeNote), 4, weatherTranscript(3))
			orig := append([]byte(nil), c.Body...)
			if resp, _ := h.Before(bg, c); resp != nil {
				t.Fatal("a note never short-circuits")
			}
			vs, notes := verdictsOf(c)
			if notes != 1 || len(vs) != 1 || vs[0].Action != contract.ModeNote || countProxy(c.Body) != 1 || !json.Valid(c.Body) {
				t.Fatalf("notes %d verdicts %+v body %s", notes, vs, c.Body)
			}
			if len(c.Body) <= len(orig) || !strings.HasPrefix(string(c.Body), string(orig[:len(orig)/2])) {
				t.Error("the note is spliced in, the rest of the body is kept")
			}
			again := before(t, w, dupRing(), dupMode(contract.ModeNote), 5, weatherTranscript(3))
			h.Before(bg, again)
			vs, notes = verdictsOf(again)
			if notes != 0 || len(vs) != 1 || vs[0].Action != contract.ModeObserve || countProxy(again.Body) != 0 {
				t.Errorf("the same note in the same session must not repeat: notes %d %+v", notes, vs)
			}
			other := before(t, w, dupRing(), dupMode(contract.ModeNote), 4, weatherTranscript(3))
			other.Session.ID = "s2"
			h.Before(bg, other)
			if _, notes := verdictsOf(other); notes != 1 {
				t.Error("dedup is per session")
			}
		})
	}
}

func TestAtMostTwoNotesPerCall(t *testing.T) {
	ring := fakeRing{
		{Index: 39, Tools: []string{"get_weather"}, ArgsHashes: []uint64{parisHash}, TextHash: 5, Shingles: []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		{Index: 40, Tools: []string{"get_weather"}, ArgsHashes: []uint64{parisHash}, TextHash: 5, Shingles: []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
	}
	h := newMon(monitors.Options{})
	c := before(t, tollgate.WireOpenAIChat, ring, attr(func(c *contract.MonitorConfig) {
		c.DuplicateToolCall.Mode, c.NoProgress.Mode, c.StepBudget.Mode = contract.ModeNote, contract.ModeNote, contract.ModeNote
	}), 41, weatherTranscript(2))
	h.Before(bg, c)
	vs, notes := verdictsOf(c)
	got := map[contract.MonitorName]contract.MonitorMode{}
	for _, v := range vs {
		got[v.Monitor] = v.Action
	}
	want := map[contract.MonitorName]contract.MonitorMode{
		contract.MonitorDuplicateToolCall: contract.ModeNote, contract.MonitorNoProgress: contract.ModeNote, contract.MonitorStepBudget: contract.ModeObserve,
	}
	if notes != 2 || countProxy(c.Body) != 2 || !reflect.DeepEqual(got, want) {
		t.Errorf("notes %d (%d in body) actions %v, want %v", notes, countProxy(c.Body), got, want)
	}
	if !json.Valid(c.Body) {
		t.Errorf("body not valid JSON: %s", c.Body)
	}
}

func TestBlockModeAnswersTheCallWithoutTheUpstream(t *testing.T) {
	for _, w := range wires {
		for _, stream := range []bool{false, true} {
			t.Run(string(w)+"/stream="+map[bool]string{true: "on", false: "off"}[stream], func(t *testing.T) {
				h := newMon(monitors.Options{})
				c := before(t, w, dupRing(), dupMode(contract.ModeBlock), 4, weatherTranscript(3))
				c.Stream = stream
				resp, err := h.Before(bg, c)
				if err != nil || resp == nil || resp.Status != 200 || countProxy(resp.Body) != 1 {
					t.Fatalf("block = %+v, %v", resp, err)
				}
				wantType := map[bool]string{true: "text/event-stream", false: "application/json"}[stream]
				if resp.Header["Content-Type"][0] != wantType || !strings.Contains(string(resp.Body), "get_weather") || !strings.Contains(string(resp.Body), `call_2`) {
					t.Errorf("response %s %s", resp.Header, resp.Body)
				}
				if b := string(resp.Body); strings.Contains(b, "tool_calls") || strings.Contains(b, "tool_use") {
					t.Errorf("a block is a terminal text turn and names no tool: %s", b)
				}
				vs, notes := verdictsOf(c)
				if notes != 0 || len(vs) != 1 || vs[0].Action != contract.ModeBlock || vs[0].Mode != contract.ModeBlock {
					t.Errorf("verdicts %+v notes %d", vs, notes)
				}
				if h.Status()["blocks"] != int64(1) {
					t.Errorf("status %v", h.Status())
				}
			})
		}
	}
}

func TestBlockWithNoEarlierIdenticalResultDegradesToANote(t *testing.T) {
	h := newMon(monitors.Options{})
	// The same call twice, but the earlier result differs: the state changed, so there is nothing to point at.
	ring := fakeRing{
		{Index: 1, Tools: []string{"get_weather"}, ArgsHashes: []uint64{parisHash}},
		{Index: 2, Tools: []string{"get_weather"}, ArgsHashes: []uint64{parisHash}},
	}
	msgs := weatherTranscript(2)
	msgs[2].Text = "rainy, 12C"
	c := before(t, tollgate.WireOpenAIChat, ring, dupMode(contract.ModeBlock), 3, msgs)
	resp, _ := h.Before(bg, c)
	vs, notes := verdictsOf(c)
	if resp != nil || notes != 1 || len(vs) != 1 || vs[0].Action != contract.ModeNote || vs[0].Mode != contract.ModeBlock {
		t.Fatalf("resp %v notes %d verdicts %+v", resp, notes, vs)
	}
	if h.Status()["blocks_degraded"] != int64(1) || countProxy(c.Body) != 1 {
		t.Errorf("status %v", h.Status())
	}
}

func TestRepeatedReadBlocksFromBlockAfterOn(t *testing.T) {
	read := func(id string) tollgate.ToolCall { return tcall(id, "read_file", `{"path":"/a.go"}`) }
	msgs := []tollgate.Message{user("t"), asst("", read("r1")), toolResult("r1", "package a"), asst("", tcall("g", "grep", `{}`)), toolResult("g", "x"),
		asst("", read("r3")), toolResult("r3", "package a")}
	sub := contract.ToolSubject("read_file", []byte(`{"path":"/a.go"}`))
	ring := fakeRing{
		{Index: 1, Tools: []string{"read_file"}, ArgsHashes: []uint64{1}, ReadHashes: []uint64{sub}},
		{Index: 2, Tools: []string{"grep"}, ArgsHashes: []uint64{2}},
		{Index: 3, Tools: []string{"read_file"}, ArgsHashes: []uint64{3}, ReadHashes: []uint64{sub}},
	}
	mode := func(blockAfter int) map[string]string {
		return attr(func(c *contract.MonitorConfig) {
			c.RepeatedRead = contract.RepeatedReadConfig{Mode: contract.ModeNote, MinReads: 2, BlockAfter: blockAfter}
		})
	}
	h := newMon(monitors.Options{})
	noted := before(t, tollgate.WireOpenAIChat, ring, mode(3), 4, msgs)
	if resp, _ := h.Before(bg, noted); resp != nil {
		t.Fatal("the second read (count 2) is only noted when blockAfter is 3")
	}
	if vs, _ := verdictsOf(noted); vs[0].Action != contract.ModeNote || vs[0].Evidence.Count != 2 {
		t.Errorf("%+v", vs)
	}
	blocked := before(t, tollgate.WireOpenAIChat, ring, mode(2), 4, msgs)
	h2 := newMon(monitors.Options{})
	resp, _ := h2.Before(bg, blocked)
	if vs, _ := verdictsOf(blocked); resp == nil || vs[0].Action != contract.ModeBlock || vs[0].Monitor != contract.MonitorRepeatedRead {
		t.Errorf("blockAfter 2 must block the second read: %v %+v", resp, vs)
	}
}

func TestPathEvidenceOnlyWhenCaptureIsAllowed(t *testing.T) {
	yes, no := true, false
	sub := contract.ToolSubject("read_file", []byte(`{"path":"/src/a.go"}`))
	ring := fakeRing{
		{Index: 1, Tools: []string{"read_file"}, ReadHashes: []uint64{sub}}, {Index: 2, Tools: []string{"read_file"}, ReadHashes: []uint64{sub}},
		{Index: 3, Tools: []string{"read_file"}, ReadHashes: []uint64{sub}},
	}
	msgs := []tollgate.Message{user("t"), asst("", tcall("r", "read_file", `{"path":"/src/a.go"}`)), toolResult("r", "x")}
	cases := []struct {
		name   string
		global bool
		policy *tollgate.Policy
		want   string
	}{
		{"capture opted in", false, &tollgate.Policy{Capture: true}, "/src/a.go"},
		{"no policy", false, nil, ""},
		{"capture off", false, &tollgate.Policy{Capture: false}, ""},
		{"global privacy mode", true, &tollgate.Policy{Capture: true}, ""},
		{"workspace privacy mode", false, &tollgate.Policy{Capture: true, PrivacyMode: &yes}, ""},
		{"workspace privacy mode explicitly off", false, &tollgate.Policy{Capture: true, PrivacyMode: &no}, "/src/a.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := before(t, tollgate.WireOpenAIChat, ring, nil, 4, msgs)
			c.Tenant.Policy = tc.policy
			newMon(monitors.Options{PrivacyMode: tc.global}).Before(bg, c)
			vs, _ := verdictsOf(c)
			var got contract.MonitorEvidence
			for _, v := range vs {
				if v.Monitor == contract.MonitorRepeatedRead {
					got = v.Evidence
				}
			}
			if got.Path != tc.want || got.PathHash != contract.HashID("ws", contract.HashValue(sub)) {
				t.Errorf("path %q hash %q", got.Path, got.PathHash)
			}
		})
	}
}

func TestBeforeDoesNothingWithoutItsInputs(t *testing.T) {
	h := newMon(monitors.Options{})
	noRing := before(t, tollgate.WireOpenAIChat, dupRing(), dupMode(contract.ModeNote), 4, weatherTranscript(3))
	delete(noRing.Meta, contract.MetaMonitors)
	orig := string(noRing.Body)
	passthrough := before(t, tollgate.WirePassthrough, dupRing(), dupMode(contract.ModeNote), 4, weatherTranscript(3))
	noMsgs := before(t, tollgate.WireOpenAIChat, dupRing(), dupMode(contract.ModeNote), 4, nil)
	for name, c := range map[string]*tollgate.Call{"no verdict pointer": noRing, "passthrough": passthrough, "no messages": noMsgs} {
		body := string(c.Body)
		if resp, err := h.Before(bg, c); resp != nil || err != nil || string(c.Body) != body {
			t.Errorf("%s: touched the call", name)
		}
	}
	if string(noRing.Body) != orig {
		t.Error("body changed")
	}
}

func TestNearDeadlineSkipsAndIsCounted(t *testing.T) {
	h := newMon(monitors.Options{})
	ctx, cancel := context.WithTimeout(bg, 2*time.Millisecond)
	defer cancel()
	c := before(t, tollgate.WireOpenAIChat, dupRing(), dupMode(contract.ModeNote), 4, weatherTranscript(3))
	h.Before(ctx, c)
	vs, notes := verdictsOf(c)
	if len(vs) != 0 || notes != 0 || countProxy(c.Body) != 0 || h.Status()["skipped_deadline"] != int64(1) {
		t.Errorf("verdicts %v notes %d status %v", vs, notes, h.Status())
	}
	far, cancel2 := context.WithTimeout(bg, time.Minute)
	defer cancel2()
	c2 := before(t, tollgate.WireOpenAIChat, dupRing(), nil, 4, weatherTranscript(3))
	h.Before(far, c2)
	if vs, _ := verdictsOf(c2); len(vs) != 1 {
		t.Error("a far deadline must not skip")
	}
}

func TestCancelledChainWritesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(bg)
	cancel()
	h := newMon(monitors.Options{})
	c := before(t, tollgate.WireOpenAIChat, dupRing(), dupMode(contract.ModeBlock), 4, weatherTranscript(3))
	orig := string(c.Body)
	resp, _ := h.Before(ctx, c)
	vs, notes := verdictsOf(c)
	if resp != nil || len(vs) != 0 || notes != 0 || string(c.Body) != orig {
		t.Errorf("a hook the chain gave up on must leave no trace: %v %v %d", resp, vs, notes)
	}
}

func TestConfigChangeTakesEffectOnTheNextCall(t *testing.T) {
	h := newMon(monitors.Options{})
	first := before(t, tollgate.WireOpenAIChat, dupRing(), dupMode(contract.ModeObserve), 4, weatherTranscript(3))
	h.Before(bg, first)
	second := before(t, tollgate.WireOpenAIChat, dupRing(), dupMode(contract.ModeNote), 4, weatherTranscript(3))
	second.Session.ID = "s9"
	h.Before(bg, second)
	_, n1 := verdictsOf(first)
	_, n2 := verdictsOf(second)
	if n1 != 0 || n2 != 1 {
		t.Errorf("notes %d then %d: the tenant attr is read per call, no restart", n1, n2)
	}
	bad := before(t, tollgate.WireOpenAIChat, dupRing(), map[string]string{"monitors": "{not json"}, 4, weatherTranscript(3))
	h.Before(bg, bad)
	if vs, _ := verdictsOf(bad); len(vs) != 1 || vs[0].Mode != contract.ModeObserve {
		t.Errorf("a bad attr is the defaults: %+v", vs)
	}
}

func TestStatusCountsFires(t *testing.T) {
	h := newMon(monitors.Options{})
	c := before(t, tollgate.WireOpenAIChat, dupRing(), nil, 4, weatherTranscript(3))
	h.Before(bg, c)
	st := h.Status()
	if st["fires_duplicate_tool_call"] != int64(1) || st["fires_repeated_read"] != int64(0) {
		t.Errorf("%v", st)
	}
	for _, k := range []string{"skipped_deadline", "notes_not_adopted", "blocks_degraded"} {
		if _, ok := st[k]; !ok {
			t.Errorf("status lacks %q", k)
		}
	}
}

func TestWithoutAnEditorEveryNoteAndBlockDegradesToObserve(t *testing.T) {
	for _, mode := range []contract.MonitorMode{contract.ModeNote, contract.ModeBlock} {
		t.Run(string(mode), func(t *testing.T) {
			h := monitors.New(monitors.Options{}) // the host forgot to inject wire.Editor
			c := before(t, tollgate.WireOpenAIChat, dupRing(), dupMode(mode), 4, weatherTranscript(3))
			orig := string(c.Body)
			resp, err := h.Before(bg, c)
			vs, notes := verdictsOf(c)
			if resp != nil || err != nil || string(c.Body) != orig || notes != 0 || len(vs) != 1 || vs[0].Action != contract.ModeObserve {
				t.Errorf("resp %v err %v notes %d verdicts %+v", resp, err, notes, vs)
			}
			if h.Status()["notes_dropped"] == int64(0) {
				t.Errorf("the dropped note must be counted: %v", h.Status())
			}
		})
	}
}

func readEntry(idx int, path string) contract.CallSummary {
	args := []byte(`{"path":"` + path + `"}`)
	return contract.CallSummary{Index: idx, Tools: []string{"read_file"}, ArgsHashes: []uint64{contract.ToolArgsHash("read_file", args)},
		ReadHashes: []uint64{contract.ToolSubject("read_file", args)}, Input: 1000, Output: 50, BodyBytes: 4000 * idx, UsageKnown: true}
}

// pendingTurn is a pending turn that runs the tools in order: each is (name, path).
func pendingTurn(idx int, tools ...[2]string) contract.CallSummary {
	s := contract.CallSummary{Index: idx}
	for _, tl := range tools {
		args := []byte(`{"path":"` + tl[1] + `"}`)
		s.Tools = append(s.Tools, tl[0])
		s.ArgsHashes = append(s.ArgsHashes, contract.ToolArgsHash(tl[0], args))
		switch contract.ClassifyTool(tl[0]) {
		case contract.ToolRead:
			s.ReadHashes = append(s.ReadHashes, contract.ToolSubject(tl[0], args))
		case contract.ToolWrite:
			s.WroteHashes = append(s.WroteHashes, contract.ToolSubject(tl[0], args))
		}
	}
	return s
}

// M2 (the verifier's probe): the pending turn wrote main.go and then read it back (edit and verify). Its own
// write breaks the pattern, so neither duplicate_tool_call nor repeated_read fires. A read before the
// write in the same turn is still a repeat.
func TestPendingTurnsOwnWriteBeforeAReadBreaksThePattern(t *testing.T) {
	cfg := contract.DefaultMonitorConfig()
	cases := []struct {
		name string
		cur  contract.CallSummary
		want []contract.MonitorName
	}{
		{"write then read: edit and verify", pendingTurn(3, [2]string{"write_file", "main.go"}, [2]string{"read_file", "main.go"}), nil},
		{"read then write: the read repeats", pendingTurn(3, [2]string{"read_file", "main.go"}, [2]string{"write_file", "main.go"}),
			[]contract.MonitorName{contract.MonitorDuplicateToolCall, contract.MonitorRepeatedRead}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ring := fakeRing{readEntry(1, "main.go"), readEntry(2, "main.go"), tc.cur}
			c := before(t, tollgate.WireOpenAIChat, ring, nil, 4, []tollgate.Message{user("x")})
			var got []contract.MonitorName
			for _, d := range monitors.Registry() {
				if d.Stream {
					continue
				}
				if v, ok := d.Detect(c, ring, cfg); ok {
					got = append(got, v.Monitor)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("fired %v, want %v", got, tc.want)
			}
		})
	}
}
