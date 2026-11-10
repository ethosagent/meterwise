package monitors_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/history"
	"github.com/ethosagent/meterwise/internal/monitors"
)

// B2: the ring is keyed and ordered by call index. These drive history and monitors hook by hook, the
// way tollgate orders them, with the races a fast client and a fast (blocked) call produce.

type driver struct {
	t     *testing.T
	h     *history.Hook
	m     *monitors.Hook
	attrs map[string]string
	n     int // calls started
}

func newDriver(t *testing.T, attrs map[string]string) *driver {
	return &driver{t: t, h: history.New(history.Options{}), m: newMon(monitors.Options{}), attrs: attrs}
}

// start runs Before (history then monitors) for the session's next call with msgs.
func (d *driver) start(msgs []tollgate.Message) *tollgate.Call {
	d.n++
	c := &tollgate.Call{
		ID: fmt.Sprintf("c%d", d.n), Tenant: tollgate.Tenant{ID: "ws", Attrs: d.attrs},
		Session: tollgate.Session{ID: "s", TenantID: "ws", Calls: d.n}, Provider: tollgate.Provider{Wire: tollgate.WireOpenAIChat},
		Stream: true, Messages: msgs, Body: requestBody(d.t, tollgate.WireOpenAIChat, true, msgs),
	}
	if _, err := d.h.Before(context.Background(), c); err != nil {
		d.t.Fatal(err)
	}
	if _, err := d.m.Before(context.Background(), c); err != nil {
		d.t.Fatal(err)
	}
	return c
}

// stream feeds the response turn as stream events.
func (d *driver) stream(c *tollgate.Call, turn tollgate.Message) {
	for i, tc := range turn.ToolCalls {
		e := tollgate.Event{Kind: tollgate.EventToolCallDelta, ToolCall: &tollgate.ToolCallDelta{Index: i, ID: tc.ID, Name: tc.Name, Arguments: string(tc.Arguments)}}
		d.h.OnEvent(context.Background(), c, e)
		d.m.OnEvent(context.Background(), c, e)
	}
}

func (d *driver) finish(c *tollgate.Call) {
	m := tollgate.Metric{Finish: tollgate.FinishOK, Status: 200, Stream: true, Usage: tollgate.Usage{InputTokens: 100, OutputTokens: 10}}
	d.h.After(context.Background(), c, m)
	d.m.After(context.Background(), c, m)
}

func ringIdx(c *tollgate.Call) []int {
	var out []int
	for _, s := range c.Meta[contract.MetaRing].(contract.RingView).Calls() {
		out = append(out, s.Index)
	}
	return out
}

func fired(c *tollgate.Call) []contract.MonitorVerdict {
	vs, _ := c.Meta[contract.MetaMonitors].(*contract.CallVerdicts).Seal()
	return vs
}

func firedOf(vs []contract.MonitorVerdict, m contract.MonitorName) *contract.MonitorVerdict {
	for i := range vs {
		if vs[i].Monitor == m {
			return &vs[i]
		}
	}
	return nil
}

func cityTurn(k int, city string) tollgate.Message {
	return asst("", tcall(fmt.Sprintf("t%d", k), "get_weather", `{"city":"`+city+`"}`))
}

// fastClient: calls 1..last-1 finish normally; the last call is answered and the client sends the next
// request before the last call's After. turns[k-1] is call k's response.
func fastClient(t *testing.T, turns []string) (*tollgate.Call, []contract.MonitorVerdict) {
	d := newDriver(t, nil)
	msgs := []tollgate.Message{user("go")}
	var lastCall *tollgate.Call
	for k := 1; k <= len(turns); k++ {
		c := d.start(msgs)
		turn := cityTurn(k, turns[k-1])
		d.stream(c, turn)
		if k < len(turns) {
			d.finish(c)
		} else {
			lastCall = c
		}
		msgs = append(msgs, turn, toolResult(turn.ToolCalls[0].ID, "w "+turns[k-1]))
	}
	next := d.start(msgs)
	vs := fired(next)
	d.finish(lastCall)
	return next, vs
}

func cities(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("City%d", i+1)
	}
	return out
}

// The verifier's n = 51/60 race: past RingDepth calls the request that beats the previous After must
// still see that call, and the duplicate it makes must fire.
func TestCatchUpWorksPastTheRingDepth(t *testing.T) {
	for _, n := range []int{10, 49, 50, 51, 60, 120} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			turns := cities(n)
			turns[n-3], turns[n-1] = "Paris", "Paris" // call n repeats call n-2
			next, vs := fastClient(t, turns)
			idx := ringIdx(next)
			want := make([]int, 0, history.RingDepth)
			for i := max(1, n-history.RingDepth+1); i <= n; i++ {
				want = append(want, i)
			}
			if !slices.Equal(idx, want) {
				t.Errorf("ring = %v, want the %d newest call indexes ending at %d", idx, len(want), n)
			}
			v := firedOf(vs, contract.MonitorDuplicateToolCall)
			if v == nil || !slices.Equal(v.Evidence.MatchedCalls, []int{n - 2, n}) {
				t.Errorf("duplicate_tool_call = %+v, want matched [%d %d]", v, n-2, n)
			}
		})
	}
}

// The verifier's out-of-order After: call 5 finishes (a block takes microseconds) before call 4's After.
// The ring must hold each index once, in order, and nothing may fire on distinct calls.
func TestOutOfOrderAfterKeepsOneEntryPerIndex(t *testing.T) {
	d := newDriver(t, nil)
	msgs := []tollgate.Message{user("go")}
	turns := []string{"A", "B", "C", "D", "E"}
	var c4 *tollgate.Call
	for k := 1; k <= 5; k++ {
		c := d.start(msgs)
		turn := cityTurn(k, turns[k-1])
		d.stream(c, turn)
		switch k {
		case 4:
			c4 = c
		case 5:
			d.finish(c)
			d.finish(c4)
		default:
			d.finish(c)
		}
		msgs = append(msgs, turn, toolResult(turn.ToolCalls[0].ID, "w "+turns[k-1]))
	}
	c6 := d.start(msgs)
	if idx := ringIdx(c6); !slices.Equal(idx, []int{1, 2, 3, 4, 5}) {
		t.Errorf("ring at call 6 = %v, want [1 2 3 4 5]", idx)
	}
	if vs := fired(c6); len(vs) != 0 {
		t.Errorf("every call was distinct, yet %+v fired", vs)
	}
}

// The verifier's prefill client: every request ends with an assistant prefill. It is not a turn: it must
// not replace the newest real turn, no_progress must not fire on distinct steps (only at call 6, whose two
// newest turns are the same text and the same call), and the Paris repeat (calls 4 and 5, pending at
// call 6) must fire.
func TestAssistantPrefillIsNotATurn(t *testing.T) {
	r := newRig(t, attr(func(*contract.MonitorConfig) {}), monitors.Options{})
	msgs := []tollgate.Message{user("go")}
	turns := []string{"A", "B", "C", "Paris", "Paris", "D", "E"}
	var noProgressAt, dupAt []int
	for k := 1; k <= len(turns); k++ {
		req := append(append([]tollgate.Message(nil), msgs...), asst("Here is my next step, as JSON:"))
		rep := r.call(tollgate.WireAnthropicMessages, false, req)
		for _, v := range rep.seen.verdicts {
			switch v.Monitor {
			case contract.MonitorNoProgress:
				noProgressAt = append(noProgressAt, k)
			case contract.MonitorDuplicateToolCall:
				dupAt = append(dupAt, k)
			}
		}
		turn := cityTurn(k, turns[k-1])
		turn.Text = "Here is my next step, as JSON: calling the weather tool for " + turns[k-1]
		msgs = append(msgs, turn, toolResult(turn.ToolCalls[0].ID, "w "+turns[k-1]))
	}
	if len(noProgressAt) > 1 || (len(noProgressAt) == 1 && noProgressAt[0] != 6) {
		t.Errorf("no_progress fired at calls %v: only call 6 repeats a step", noProgressAt)
	}
	if !slices.Equal(dupAt, []int{6}) {
		t.Errorf("duplicate_tool_call fired at calls %v, want [6] (call 6 carries the Paris repeat)", dupAt)
	}
}

// The same prefill client through a real model upstream on both wires, stream on and off.
func TestAssistantPrefillOnBothWiresAndStreamModes(t *testing.T) {
	for _, v := range variants() {
		t.Run(v.String(), func(t *testing.T) {
			r, up := loopRig(t, attr(func(*contract.MonitorConfig) {}))
			up.push("A", "B", "C", "Paris", "Paris", "D", "E")
			msgs := []tollgate.Message{user("go")}
			var dupAt []int
			for k := 1; k <= 7; k++ {
				req := append(append([]tollgate.Message(nil), msgs...), asst("Next step:"))
				rep := r.call(v.wire, v.stream, req)
				for _, x := range rep.seen.verdicts {
					if x.Monitor == contract.MonitorDuplicateToolCall {
						dupAt = append(dupAt, k)
					} else {
						t.Errorf("call %d: unexpected %s %+v", k, x.Monitor, x.Evidence)
					}
				}
				turn, _ := sdkTurn(t, v, rep.body)
				msgs = append(msgs, turn, toolResult(turn.ToolCalls[0].ID, "sunny"))
			}
			if !slices.Equal(dupAt, []int{6}) {
				t.Errorf("duplicate_tool_call fired at calls %v, want [6]", dupAt)
			}
		})
	}
}
