package monitors_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/history"
	"github.com/ethosagent/meterwise/internal/monitors"
)

// K4: the per-call cost of a WARM session (one session, every call the next of it), not a fresh session
// per iteration like BenchmarkBefore200Messages. Three shapes of what the ring does on a call:
//
//	steady   the previous call's After ran: the ring is current, Before reads nothing from the request.
//	catchup  the previous call's After has not run (a client faster than After): Before reads that one turn.
//	heal     the previous call left an empty entry (answered without a stream): Before checks the request is
//	         aligned with the ring (side-request guard, S1) and heals the entry.
//
// A call's request is a sliding window of a cyclic pool of distinct turns, so every call carries the turns
// its predecessor did, shifted by one.

const warmWindowPairs = 100

// warmPool is poolPairs assistant/tool pairs, each with its own tool call, text and result.
func warmPool(poolPairs int) []tollgate.Message {
	msgs := make([]tollgate.Message, 0, 2*poolPairs)
	for i := 0; i < poolPairs; i++ {
		id := fmt.Sprintf("call_%d", i)
		args := fmt.Sprintf(`{"query":"symbol_%d","path":"/src/module_%d/file_%d.go"}`, i, i%7, i)
		msgs = append(msgs, asst(fmt.Sprintf("Searching module %d for the symbol the linker reported.", i), tcall(id, "grep", args)),
			toolResult(id, fmt.Sprintf("match in file_%d line 42; ", i)))
	}
	return msgs
}

// warmSession drives one session through history and monitors. step(i) runs call i.
type warmSession struct {
	h    *history.Hook
	m    *monitors.Hook
	pool []tollgate.Message
	body []byte
}

func newWarmSession(b *testing.B) *warmSession {
	pool := warmPool(2000)
	return &warmSession{h: history.New(history.Options{}), m: newMon(monitors.Options{}), pool: pool, body: requestBody(b, tollgate.WireOpenAIChat, true, pool[:8])}
}

// call is call i of the session: its request ends with the result of the turn call i-1 made.
func (s *warmSession) call(i int) *tollgate.Call {
	at := 2 * (i % (len(s.pool)/2 - warmWindowPairs))
	msgs := s.pool[at : at+2*warmWindowPairs]
	return &tollgate.Call{
		ID: "warm", Tenant: tollgate.Tenant{ID: "ws", Attrs: dupMode(contract.ModeNote)}, Session: tollgate.Session{ID: "warm", TenantID: "ws", Calls: i},
		Provider: tollgate.Provider{Wire: tollgate.WireOpenAIChat}, Stream: true, Messages: msgs, Body: s.body,
	}
}

func (s *warmSession) before(c *tollgate.Call) { beforeBoth(context.Background(), s.h, s.m, c) }

// after finishes call c: with the turn of its own (stream events) or without (an empty entry).
func (s *warmSession) after(c *tollgate.Call, withTurn bool) {
	ctx := context.Background()
	if withTurn {
		turn := s.pool[2*((c.Session.Calls)%(len(s.pool)/2-warmWindowPairs)+warmWindowPairs)]
		for i, tc := range turn.ToolCalls {
			e := tollgate.Event{Kind: tollgate.EventToolCallDelta, ToolCall: &tollgate.ToolCallDelta{Index: i, ID: tc.ID, Name: tc.Name, Arguments: string(tc.Arguments)}}
			s.h.OnEvent(ctx, c, e)
			s.m.OnEvent(ctx, c, e)
		}
	}
	met := tollgate.Metric{Finish: tollgate.FinishOK, Status: 200, Stream: true, Usage: tollgate.Usage{InputTokens: 100, OutputTokens: 10}}
	s.h.After(ctx, c, met)
	s.m.After(ctx, c, met)
}

func BenchmarkWarmSession(b *testing.B) {
	for _, shape := range []string{"steady", "catchup", "heal"} {
		b.Run(shape, func(b *testing.B) {
			s := newWarmSession(b)
			step := func(i int) {
				c := s.call(i)
				s.before(c)
				switch shape {
				case "steady":
					s.after(c, true)
				case "heal":
					s.after(c, false)
				}
			}
			for i := warmWindowPairs; i < warmWindowPairs+8; i++ { // warm the session up before timing
				step(i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				step(warmWindowPairs + 8 + i)
			}
		})
	}
}
