package monitors_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/monitors"
)

type variant struct {
	wire   tollgate.WireFormat
	stream bool
}

func (v variant) String() string { return fmt.Sprintf("%s/stream=%v", v.wire, v.stream) }

func variants() []variant {
	var out []variant
	for _, w := range wires {
		out = append(out, variant{w, false}, variant{w, true})
	}
	return out
}

// walk plays the agent that asks for the same weather: call k carries k-1 turns of history.
func walk(t *testing.T, r *rig, v variant, calls int) []reply {
	t.Helper()
	var out []reply
	for k := 1; k <= calls; k++ {
		out = append(out, r.call(v.wire, v.stream, weatherTranscript(k-1)))
	}
	return out
}

// inserted returns what out adds to in, failing when the bytes around it differ.
func inserted(t *testing.T, in, out []byte) string {
	t.Helper()
	p := 0
	for p < len(in) && p < len(out) && in[p] == out[p] {
		p++
	}
	s := 0
	for s < len(in)-p && in[len(in)-1-s] == out[len(out)-1-s] {
		s++
	}
	if p+s < len(in) {
		t.Fatalf("bytes outside the splice changed")
	}
	return string(out[p : len(out)-s])
}

func TestObserveLeavesEveryByteAlone(t *testing.T) {
	for _, v := range variants() {
		t.Run(v.String(), func(t *testing.T) {
			r := newRig(t, nil, monitors.Options{})
			reps := walk(t, r, v, 4)
			ups := r.upstreamBodies()
			if len(ups) != 4 {
				t.Fatalf("upstream saw %d requests, want 4", len(ups))
			}
			for i, rep := range reps {
				if string(ups[i]) != string(rep.sent) || rep.status != 200 {
					t.Errorf("call %d: upstream body changed under observe (status %d)", i+1, rep.status)
				}
			}
			fired := reps[2].seen.verdicts // the third call carries the second identical tool call
			if len(fired) != 1 || fired[0].Monitor != contract.MonitorDuplicateToolCall || fired[0].Action != contract.ModeObserve {
				t.Fatalf("call 3 verdicts = %+v", fired)
			}
			if !reflect.DeepEqual(fired[0].Evidence.MatchedCalls, []int{1, 2}) || fired[0].Evidence.Count != 2 {
				t.Errorf("evidence = %+v", fired[0].Evidence)
			}
			if len(reps[0].seen.verdicts)+len(reps[1].seen.verdicts) != 0 {
				t.Error("nothing fires before the second identical call")
			}
		})
	}
}

func TestNoteIsExactlyOneSplicedNoteAndNotRepeated(t *testing.T) {
	for _, v := range variants() {
		t.Run(v.String(), func(t *testing.T) {
			r := newRig(t, dupMode(contract.ModeNote), monitors.Options{})
			reps := walk(t, r, v, 4)
			ups := r.upstreamBodies()
			third := reps[2]
			if third.seen.notes != 1 || len(third.seen.verdicts) != 1 || third.seen.verdicts[0].Action != contract.ModeNote {
				t.Fatalf("call 3: notes %d verdicts %+v", third.seen.notes, third.seen.verdicts)
			}
			if countProxy(ups[2]) != 1 || !json.Valid(ups[2]) {
				t.Fatalf("upstream body must hold exactly one note and stay valid JSON: %s", ups[2])
			}
			if ins := inserted(t, third.sent, ups[2]); !strings.Contains(ins, "[proxy]") || len(ins) > 700 {
				t.Errorf("unexpected insertion %q", ins)
			}
			for _, i := range []int{0, 1, 3} {
				if string(ups[i]) != string(reps[i].sent) {
					t.Errorf("call %d must reach the upstream unchanged (the repeat is already noted in this session)", i+1)
				}
			}
			if reps[3].seen.notes != 0 || len(reps[3].seen.verdicts) != 1 || reps[3].seen.verdicts[0].Action != contract.ModeObserve {
				t.Errorf("call 4 repeats the same finding: %+v", reps[3].seen)
			}
			if third.status != 200 {
				t.Errorf("the client sees the upstream answer unchanged: %d", third.status)
			}
		})
	}
}

func TestBlockAnswersWithoutCallingTheUpstream(t *testing.T) {
	for _, v := range variants() {
		t.Run(v.String(), func(t *testing.T) {
			r := newRig(t, dupMode(contract.ModeBlock), monitors.Options{})
			walk(t, r, v, 2)
			upBefore := len(r.up.Requests())
			blocked := r.call(v.wire, v.stream, weatherTranscript(2))
			if got := len(r.up.Requests()); got != upBefore {
				t.Fatalf("a blocked call must not reach the upstream: %d -> %d requests", upBefore, got)
			}
			if blocked.status != 200 {
				t.Fatalf("status %d: %s", blocked.status, blocked.body)
			}
			assertBlockResponse(t, v, blocked)
			vs := blocked.seen.verdicts
			if len(vs) != 1 || vs[0].Action != contract.ModeBlock || vs[0].Mode != contract.ModeBlock || blocked.seen.notes != 0 {
				t.Errorf("verdicts %+v notes %d", vs, blocked.seen.notes)
			}
			if blocked.seen.usage != (tollgate.Usage{}) {
				t.Errorf("a blocked call has no usage: %+v", blocked.seen.usage)
			}
			if s := vs[0].Basis; s.PriorInputTokens == 0 && s.BodyDeltaBytes == 0 {
				t.Errorf("the estimate basis must be filled: %+v", s)
			}
			// the same subject again in this session: noted, sent upstream, never a second block
			again := r.call(v.wire, v.stream, weatherTranscript(3))
			if len(r.up.Requests()) != upBefore+1 || again.seen.notes != 1 || again.seen.verdicts[0].Action != contract.ModeNote {
				t.Errorf("the second repeat must be a note that reaches the upstream: %+v", again.seen)
			}
			if c := r.mon.Status()["blocks_capped"]; c != int64(1) {
				t.Errorf("blocks_capped = %v", c)
			}
		})
	}
}

// assertBlockResponse checks the block is a valid answer in the call's wire and stream mode.
func assertBlockResponse(t *testing.T, v variant, rep reply) {
	t.Helper()
	ct := rep.header.Get("Content-Type")
	body := string(rep.body)
	if countProxy(rep.body) != 1 || !strings.Contains(body, "get_weather") || !strings.Contains(body, "call_1") {
		t.Errorf("block body lacks the note or the earlier result's location: %s", body)
	}
	if strings.Contains(body, "tool_calls") || strings.Contains(body, "tool_use") || strings.Contains(body, "Paris") {
		t.Errorf("a block is a terminal text turn: no tool call, nothing replayed: %s", body)
	}
	if !v.stream {
		var doc map[string]any
		if ct != "application/json" || json.Unmarshal(rep.body, &doc) != nil {
			t.Errorf("want one JSON document, got %q: %s", ct, body)
		}
		return
	}
	if !strings.HasPrefix(ct, "text/event-stream") || !strings.HasSuffix(body, "\n\n") {
		t.Errorf("want whole SSE frames, got %q", ct)
	}
	for _, f := range strings.Split(strings.TrimSuffix(body, "\n\n"), "\n\n") {
		data := f[strings.Index(f, "data: ")+6:]
		var x any
		if data != "[DONE]" && json.Unmarshal([]byte(data), &x) != nil {
			t.Errorf("bad frame %q", f)
		}
	}
}

func TestRecreatedHistoryGivesTheSameVerdictsAndBlock(t *testing.T) {
	for _, v := range variants() {
		t.Run(v.String(), func(t *testing.T) {
			run := func(restartAfter int) reply {
				r := newRig(t, dupMode(contract.ModeBlock), monitors.Options{})
				var last reply
				for k := 1; k <= 3; k++ {
					if k == restartAfter+1 {
						r.hist.cur.Store(newHistory()) // the process died and came back mid-session
					}
					last = r.call(v.wire, v.stream, weatherTranscript(k-1))
				}
				return last
			}
			cont, restarted := run(0), run(2)
			strip := func(vs []contract.MonitorVerdict) []contract.MonitorVerdict {
				for i := range vs {
					vs[i].Basis = contract.EstBasis{} // token counts are not recoverable from messages
				}
				return vs
			}
			if !reflect.DeepEqual(strip(cont.seen.verdicts), strip(restarted.seen.verdicts)) || len(cont.seen.verdicts) != 1 {
				t.Errorf("verdicts differ after a history restart\n live      %+v\n restarted %+v", cont.seen.verdicts, restarted.seen.verdicts)
			}
			if string(cont.body) != string(restarted.body) {
				t.Errorf("the block response differs after a restart")
			}
		})
	}
}

func TestPrivacyNothingFromTheConversationReachesVerdicts(t *testing.T) {
	secret := "hunter2-secret-token-for-acme-corp"
	msgs := []tollgate.Message{user("my password is " + secret)}
	args := `{"city":"Paris","note":"` + secret + `"}`
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("call_%d", i)
		msgs = append(msgs, asst("thinking about "+secret, tcall(id, "get_weather", args)), toolResult(id, "result "+secret))
	}
	r := newRig(t, attr(func(c *contract.MonitorConfig) {
		c.DuplicateToolCall.Mode, c.NoProgress.Mode = contract.ModeObserve, contract.ModeNote
	}), monitors.Options{})
	for k := 0; k <= 3; k++ {
		rep := r.call(tollgate.WireOpenAIChat, true, msgs[:1+2*k])
		b, _ := json.Marshal(rep.seen.verdicts)
		if strings.Contains(string(b), secret) || strings.Contains(string(b), "acme") || strings.Contains(string(b), "Paris") {
			t.Errorf("call %d: verdicts leak message text: %s", k+1, b)
		}
		if k == 3 && len(rep.seen.verdicts) == 0 {
			t.Error("the scenario must fire something for this test to mean anything")
		}
		if countProxy(rep.seen.body) > 0 && strings.Contains(string(rep.seen.body[len(rep.sent):]), secret) {
			t.Error("a note must not repeat message text")
		}
	}
}

func newHistory() *historyHook { return newHistoryHook() }
