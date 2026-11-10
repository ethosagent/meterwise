package monitors_test

import (
	"slices"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/monitors"
)

// S1: a side request (title generation, another conversation) that shares the session id leaves an
// empty ring entry. The next main call must not heal that entry with the main thread's newest turn,
// which the ring already holds one index lower: that copy is a false duplicate_tool_call.

// sideRun streams the main thread's turns through the driver, sends a non-stream side request after the
// main turn at sideAfter (1-based), and returns every call's verdicts, keyed by call index.
func sideRun(t *testing.T, turns []string, sideAfter int) map[int][]contract.MonitorVerdict {
	d := newDriver(t, nil)
	main := []tollgate.Message{user("weather tour")}
	got := map[int][]contract.MonitorVerdict{}
	for i, city := range turns {
		c := d.start(main)
		got[d.n] = fired(c)
		turn := cityTurn(d.n, city)
		d.stream(c, turn)
		d.finish(c)
		main = append(main, turn, toolResult(turn.ToolCalls[0].ID, "w "+city))
		if i+1 == sideAfter {
			s := d.start([]tollgate.Message{user("Give this chat a title")})
			got[d.n] = fired(s)
			d.finish(s)
		}
	}
	c := d.start(main)
	got[d.n] = fired(c)
	return got
}

// The verifier's probe (reviews/06-opus-r1-evidence, TestProbeConcernC_SideCallDriver).
func TestSideRequestInTheSessionIsNotADuplicate(t *testing.T) {
	got := sideRun(t, []string{"Rome", "Oslo", "Lima", "Paris", "Kyiv"}, 4)
	for i, vs := range got {
		for _, v := range vs {
			if v.Monitor == contract.MonitorDuplicateToolCall {
				t.Errorf("call %d: false duplicate_tool_call matched=%v (every city is distinct)", i, v.Evidence.MatchedCalls)
			}
		}
	}
}

// The same with the rig (real wire bodies) and a non-stream main thread, whose entries the heal fills.
// The stream variant is the driver test above: the rig's streamed fixture answers the same tool call every
// time, a real duplicate, so it cannot tell a false one from it (K5).
func TestSideRequestInTheSessionIsNotADuplicateOnTheRig(t *testing.T) {
	r := newRig(t, dupMode(contract.ModeNote), monitors.Options{})
	main := []tollgate.Message{user("weather tour")}
	k := 0
	for i, city := range []string{"Rome", "Oslo", "Lima", "Paris", "Kyiv"} {
		k++
		noDup(t, k, r.call(tollgate.WireOpenAIChat, false, main).seen.verdicts)
		if i == 3 {
			k++
			noDup(t, k, r.call(tollgate.WireOpenAIChat, false, []tollgate.Message{user("Give this chat a title")}).seen.verdicts)
		}
		turn := cityTurn(k, city)
		main = append(main, turn, toolResult(turn.ToolCalls[0].ID, "w "+city))
	}
	noDup(t, k+1, r.call(tollgate.WireOpenAIChat, false, main).seen.verdicts)
}

func noDup(t *testing.T, call int, vs []contract.MonitorVerdict) {
	t.Helper()
	if v := firedOf(vs, contract.MonitorDuplicateToolCall); v != nil {
		t.Errorf("call %d: false duplicate_tool_call matched=%v", call, v.Evidence.MatchedCalls)
	}
}

// The main thread's own repeat still fires with a side request in between.
func TestMainThreadRepeatStillFiresAroundASideRequest(t *testing.T) {
	got := sideRun(t, []string{"Rome", "Oslo", "Paris", "Paris"}, 4)
	var hit *contract.MonitorVerdict
	for i := 1; i <= len(got); i++ {
		if v := firedOf(got[i], contract.MonitorDuplicateToolCall); v != nil {
			if slices.Contains(v.Evidence.MatchedCalls, 5) {
				t.Errorf("call %d: matched %v names the side request (call 5)", i, v.Evidence.MatchedCalls)
			}
			if hit == nil {
				hit = v
			}
		}
	}
	if hit == nil {
		t.Fatal("the main thread's Paris, Paris repeat did not fire")
	}
	if !slices.Equal(hit.Evidence.MatchedCalls, []int{3, 4}) {
		t.Errorf("matched %v, want [3 4]", hit.Evidence.MatchedCalls)
	}
}
