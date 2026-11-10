package history_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/history"
)

func TestBeforeStampsRingAndVerdictPointer(t *testing.T) {
	h := history.New(history.Options{})
	c := newCall("ws", "s1", 1)
	if r, err := h.Before(bg, c); r != nil || err != nil {
		t.Fatalf("history never short-circuits: %v %v", r, err)
	}
	if got := ringOf(t, c); len(got) != 0 {
		t.Errorf("fresh session ring = %v", got)
	}
	vs, ok := c.Meta[contract.MetaMonitors].(*contract.CallVerdicts)
	if !ok || vs == nil {
		t.Fatalf("Meta[%q] = %T, want *CallVerdicts", contract.MetaMonitors, c.Meta[contract.MetaMonitors])
	}
	c2 := newCall("ws", "s1", 2)
	h.Before(bg, c2)
	if c2.Meta[contract.MetaMonitors] == c.Meta[contract.MetaMonitors] {
		t.Error("each call gets its own verdict collection")
	}
}

func TestBeforeSkipsPassthroughAndSessionless(t *testing.T) {
	h := history.New(history.Options{})
	p := newCall("ws", "s1", 1)
	p.Provider.Wire = tollgate.WirePassthrough
	h.Before(bg, p)
	n := newCall("ws", "", 1, asst("x"))
	h.Before(bg, n)
	for _, c := range []*tollgate.Call{p, n} {
		if c.Meta != nil {
			t.Errorf("Meta stamped on a call history cannot track: %v", c.Meta)
		}
	}
}

func TestAfterAppendsTheStreamedTurn(t *testing.T) {
	h := history.New(history.Options{})
	run(t, h, newCall("ws", "s1", 1), 100, 7,
		text("Let me look "), text("at the file."),
		toolDelta(1, "edit_file", `{"path":"/b.go"}`), // out of order on purpose
		toolDelta(0, "read_file", `{"path":`), toolDelta(0, "", `"/a.go","n":1}`))
	c := newCall("ws", "s1", 2)
	h.Before(bg, c)
	got := ringOf(t, c)
	if len(got) != 1 {
		t.Fatalf("ring = %+v", got)
	}
	s := got[0]
	want := contract.CallSummary{
		Index: 1, Tools: []string{"read_file", "edit_file"},
		ArgsHashes:  []uint64{contract.ToolArgsHash("read_file", []byte(`{"n":1,"path":"/a.go"}`)), contract.ToolArgsHash("edit_file", []byte(`{"path":"/b.go"}`))},
		ReadHashes:  []uint64{contract.HashString("/a.go")},
		WroteHashes: []uint64{contract.HashString("/b.go")},
		TextHash:    contract.NormalizedHash("let me look at the file"),
		Input:       100, Output: 7, BodyBytes: len(c.Body),
	}
	s.Shingles = nil
	if !reflect.DeepEqual(s, want) {
		t.Errorf("summary\n got  %+v\n want %+v", s, want)
	}
}

func TestFailedCallAddsNothing(t *testing.T) {
	h := history.New(history.Options{})
	c := newCall("ws", "s1", 1)
	h.Before(bg, c)
	h.After(bg, c, tollgate.Metric{Finish: tollgate.FinishUpstreamError, Status: 500})
	c2 := newCall("ws", "s1", 2)
	h.Before(bg, c2)
	if got := ringOf(t, c2); len(got) != 0 {
		t.Errorf("a 500 must not enter the ring: %+v", got)
	}
}

func TestRingKeepsFiftyCallsAndShinglesOnlyOnTheNewestThree(t *testing.T) {
	h := history.New(history.Options{})
	long := "one two three four five six seven eight nine ten eleven twelve"
	for i := 1; i <= 60; i++ {
		run(t, h, newCall("ws", "s1", i), 1, 1, text(long), toolDelta(0, "grep", fmt.Sprintf(`{"q":%d}`, i)))
	}
	c := newCall("ws", "s1", 61)
	h.Before(bg, c)
	got := ringOf(t, c)
	if len(got) != history.RingDepth || got[0].Index != 11 || got[len(got)-1].Index != 60 {
		t.Fatalf("ring len %d first %d last %d", len(got), got[0].Index, got[len(got)-1].Index)
	}
	for i, s := range got {
		if has := len(s.Shingles) > 0; has != (i >= len(got)-3) {
			t.Errorf("entry %d (call %d) has shingles = %v", i, s.Index, has)
		}
	}
}

func TestRingViewIsASnapshot(t *testing.T) {
	h := history.New(history.Options{})
	run(t, h, newCall("ws", "s1", 1), 1, 1, toolDelta(0, "grep", `{}`))
	c := newCall("ws", "s1", 2)
	h.Before(bg, c)
	view := ringOf(t, c)
	run(t, h, c, 1, 1, toolDelta(0, "grep", `{"x":1}`))
	if len(view) != 1 {
		t.Errorf("a snapshot taken in Before must not grow: %d", len(view))
	}
}

func TestRebuildFromMessagesMatchesTheLiveRing(t *testing.T) {
	read := tc("c1", "read_file", `{"path":"/a.go"}`)
	grep := tc("c2", "grep", `{"q":"x"}`)
	msgs := []tollgate.Message{
		{Role: tollgate.RoleUser, Text: "task"},
		asst("Reading the file now.", read), {Role: tollgate.RoleTool, ToolCallID: "c1", Text: "package a"},
		asst("", grep), {Role: tollgate.RoleTool, ToolCallID: "c2", Text: "hit"},
	}
	live := history.New(history.Options{})
	run(t, live, newCall("ws", "s", 1), 10, 1, text("Reading the file now."), toolDelta(0, "read_file", `{"path":"/a.go"}`))
	run(t, live, newCall("ws", "s", 2), 20, 1, toolDelta(0, "grep", `{"q":"x"}`))
	a := newCall("ws", "s", 3, msgs...)
	live.Before(bg, a)

	fresh := history.New(history.Options{}) // the process restarted mid-session
	b := newCall("ws", "s", 3, msgs...)
	fresh.Before(bg, b)
	got, want := ringOf(t, b), ringOf(t, a)
	for i := range want { // token counts and sizes are unknown after a rebuild
		want[i].Input, want[i].Output, want[i].BodyBytes = 0, 0, 0
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rebuilt ring differs from the live one\n got  %+v\n want %+v", got, want)
	}
	if fresh.Status()["rebuilt"] != int64(1) || live.Status()["rebuilt"] != int64(0) {
		t.Errorf("status %v / %v", fresh.Status(), live.Status())
	}
}

func TestRebuildReadsAtMostTheLastTwoHundredMessagesAndFiftyTurns(t *testing.T) {
	var msgs []tollgate.Message
	for i := 0; i < 150; i++ {
		msgs = append(msgs, asst("", tc(fmt.Sprint(i), "grep", fmt.Sprintf(`{"i":%d}`, i))), tollgate.Message{Role: tollgate.RoleTool, ToolCallID: fmt.Sprint(i), Text: "r"})
	}
	h := history.New(history.Options{})
	c := newCall("ws", "s", 151, msgs...)
	h.Before(bg, c)
	got := ringOf(t, c)
	if len(got) != history.RingDepth || got[len(got)-1].Index != 150 || got[0].Index != 101 {
		t.Fatalf("len %d first %d last %d", len(got), got[0].Index, got[len(got)-1].Index)
	}
	if got[len(got)-1].ArgsHashes[0] != contract.ToolArgsHash("grep", []byte(`{"i":149}`)) {
		t.Error("the newest rebuilt turn must be the newest assistant message")
	}
}

func TestEmptyNewestEntryHealsFromTheNextRequest(t *testing.T) {
	h := history.New(history.Options{})
	run(t, h, newCall("ws", "s", 1), 5, 5, toolDelta(0, "grep", `{"q":1}`))
	run(t, h, newCall("ws", "s", 2), 0, 0) // answered without a stream (a block, or stream:false)
	c := newCall("ws", "s", 3, asst("replayed", tc("c9", "read_file", `{"path":"/z"}`)), tollgate.Message{Role: tollgate.RoleTool, ToolCallID: "c9", Text: "r"})
	h.Before(bg, c)
	got := ringOf(t, c)
	if len(got) != 2 || got[1].Index != 2 || !reflect.DeepEqual(got[1].Tools, []string{"read_file"}) || got[1].TextHash != contract.NormalizedHash("replayed") {
		t.Fatalf("healed ring = %+v", got)
	}
	if got[0].Tools[0] != "grep" || h.Status()["healed"] != int64(1) {
		t.Errorf("older entry must be untouched; status %v", h.Status())
	}
}

func TestSessionsAreIsolatedClosedAndBounded(t *testing.T) {
	h := history.New(history.Options{MaxSessions: 4, Shards: 1})
	run(t, h, newCall("a", "s", 1), 1, 1, toolDelta(0, "grep", `{}`))
	other := newCall("b", "s", 1)
	h.Before(bg, other)
	if len(ringOf(t, other)) != 0 {
		t.Error("the same session id under another tenant is another ring")
	}
	h.OnSessionClosed(bg, tollgate.Session{ID: "s", TenantID: "a"})
	again := newCall("a", "s", 2)
	h.Before(bg, again)
	if len(ringOf(t, again)) != 0 {
		t.Error("OnSessionClosed must evict the ring")
	}
	for i := 0; i < 10; i++ {
		run(t, h, newCall("t", fmt.Sprint(i), 1), 1, 1, toolDelta(0, "grep", `{}`))
	}
	st := h.Status()
	if st["sessions"].(int) > 4 || st["evicted"].(int64) < 6 {
		t.Errorf("bounded by MaxSessions: %v", st)
	}
}

func TestRingHoldsNoMessageText(t *testing.T) {
	secret := "hunter2-the-secret-password and the customer's address 12 Elm Street"
	path := "/home/alice/private/notes.txt"
	h := history.New(history.Options{})
	run(t, h, newCall("ws", "s", 1), 1, 1, text(secret), toolDelta(0, "read_file", `{"path":"`+path+`"}`))
	c := newCall("ws", "s", 2, asst(secret, tc("c", "read_file", `{"path":"`+path+`"}`)))
	h.Before(bg, c)
	fresh := history.New(history.Options{})
	d := newCall("ws", "s", 2, asst(secret, tc("c", "read_file", `{"path":"`+path+`"}`)))
	fresh.Before(bg, d)
	for _, ring := range [][]contract.CallSummary{ringOf(t, c), ringOf(t, d)} {
		b, _ := json.Marshal(ring)
		for _, bad := range []string{"hunter2", "Elm Street", "alice", "notes.txt", "secret"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("ring leaks %q: %s", bad, b)
			}
		}
	}
}

func TestConcurrentSessionsAreRaceFree(t *testing.T) {
	h := history.New(history.Options{MaxSessions: 64})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 1; i <= 40; i++ {
				c := newCall("ws", fmt.Sprint(g%3), i)
				run(t, h, c, 1, 1, text("hello world"), toolDelta(0, "grep", fmt.Sprintf(`{"i":%d}`, i)))
				if i%10 == 0 {
					h.OnSessionClosed(bg, tollgate.Session{ID: fmt.Sprint(g % 3), TenantID: "ws"})
				}
			}
		}()
	}
	wg.Wait()
	h.Status()
}

// A client that sends the next request before the previous call's After has run (After runs once the
// response is out) must still see that call in the ring, and the late After must not add it twice.
func TestRingCatchesUpWhenTheNextRequestArrivesBeforeAfter(t *testing.T) {
	g1, g2, g3 := tc("c1", "grep", `{"q":1}`), tc("c2", "grep", `{"q":2}`), tc("c3", "grep", `{"q":3}`)
	tool := func(id string) tollgate.Message {
		return tollgate.Message{Role: tollgate.RoleTool, ToolCallID: id, Text: "r"}
	}
	user := tollgate.Message{Role: tollgate.RoleUser, Text: "task"}
	h := history.New(history.Options{})
	run(t, h, newCall("ws", "s", 1, user), 10, 1, toolDelta(0, "grep", `{"q":1}`))

	// call 2 is in flight: its Before and events ran, its After has not
	c2 := newCall("ws", "s", 2, user, asst("", g1), tool("c1"))
	if _, err := h.Before(bg, c2); err != nil {
		t.Fatal(err)
	}
	h.OnEvent(bg, c2, toolDelta(0, "grep", `{"q":2}`))

	// call 3 arrives now; its history carries call 2's turn
	c3 := newCall("ws", "s", 3, user, asst("", g1), tool("c1"), asst("", g2), tool("c2"))
	if _, err := h.Before(bg, c3); err != nil {
		t.Fatal(err)
	}
	got := ringOf(t, c3)
	if len(got) != 2 || got[1].Index != 2 || got[1].ArgsHashes[0] != contract.ToolArgsHash("grep", []byte(`{"q":2}`)) {
		t.Fatalf("call 3 must see call 2 in its ring: %+v", got)
	}
	if h.Status()["caught_up"] != int64(1) {
		t.Errorf("status %v", h.Status())
	}

	// the late After of call 2 fills the entry instead of adding a second one
	h.After(bg, c2, tollgate.Metric{Finish: tollgate.FinishOK, Status: 200, Usage: tollgate.Usage{InputTokens: 20, OutputTokens: 2}})
	h.OnEvent(bg, c3, toolDelta(0, "grep", `{"q":3}`))
	h.After(bg, c3, tollgate.Metric{Finish: tollgate.FinishOK, Status: 200, Usage: tollgate.Usage{InputTokens: 30, OutputTokens: 3}})
	c4 := newCall("ws", "s", 4, user, asst("", g1), tool("c1"), asst("", g2), tool("c2"), asst("", g3), tool("c3"))
	h.Before(bg, c4)
	got = ringOf(t, c4)
	if len(got) != 3 || got[0].Index != 1 || got[1].Index != 2 || got[2].Index != 3 || got[1].Input != 20 {
		t.Fatalf("ring after the late After: %+v", got)
	}
}

// A client that trims its history (fewer assistant turns than the ring holds) must not change the ring.
func TestRingIsLeftAloneWhenTheRequestCarriesFewerTurns(t *testing.T) {
	h := history.New(history.Options{})
	run(t, h, newCall("ws", "s", 1), 1, 1, toolDelta(0, "grep", `{"q":1}`))
	run(t, h, newCall("ws", "s", 2), 1, 1, toolDelta(0, "grep", `{"q":2}`))
	c := newCall("ws", "s", 3, tollgate.Message{Role: tollgate.RoleUser, Text: "summary"})
	h.Before(bg, c)
	if got := ringOf(t, c); len(got) != 2 || h.Status()["caught_up"] != int64(0) {
		t.Fatalf("ring %+v status %v", got, h.Status())
	}
}

// B2: the catch-up compares the call index with the newest index the ring knows, works past RingDepth, and
// parses only the missing turn.
func TestCatchUpPastRingDepthReadsOnlyTheMissingTurn(t *testing.T) {
	h := history.New(history.Options{})
	user := tollgate.Message{Role: tollgate.RoleUser, Text: "task"}
	msgs := []tollgate.Message{user}
	for i := 1; i <= 70; i++ {
		args := fmt.Sprintf(`{"q":%d}`, i)
		if i < 70 {
			run(t, h, newCall("ws", "s", i, msgs...), 1, 1, toolDelta(0, "grep", args))
		}
		id := fmt.Sprint(i)
		msgs = append(msgs, asst("", tc(id, "grep", args)), tollgate.Message{Role: tollgate.RoleTool, ToolCallID: id, Text: "r"})
	}
	// call 70 was answered but its After has not run; call 71 arrives
	read := h.Status()["turns_read"].(int64)
	c := newCall("ws", "s", 71, msgs...)
	h.Before(bg, c)
	got := ringOf(t, c)
	if len(got) != history.RingDepth || got[0].Index != 21 || got[len(got)-1].Index != 70 {
		t.Fatalf("ring len %d first %d last %d", len(got), got[0].Index, got[len(got)-1].Index)
	}
	if got[len(got)-1].ArgsHashes[0] != contract.ToolArgsHash("grep", []byte(`{"q":70}`)) {
		t.Error("index 70 must hold call 70's turn")
	}
	if n := h.Status()["turns_read"].(int64) - read; n != 1 {
		t.Errorf("turns read for the catch-up = %d, want 1 (incremental)", n)
	}
}

// A call that failed upstream has no turn in the next request: its index must not be filled from another
// turn (which would duplicate it).
func TestFailedCallIndexIsNotFilledFromAnotherTurn(t *testing.T) {
	h := history.New(history.Options{})
	user := tollgate.Message{Role: tollgate.RoleUser, Text: "task"}
	run(t, h, newCall("ws", "s", 1, user), 1, 1, toolDelta(0, "grep", `{"q":1}`))
	turn1 := []tollgate.Message{user, asst("", tc("1", "grep", `{"q":1}`)), {Role: tollgate.RoleTool, ToolCallID: "1", Text: "r"}}
	c2 := newCall("ws", "s", 2, turn1...)
	h.Before(bg, c2)
	h.After(bg, c2, tollgate.Metric{Finish: tollgate.FinishUpstreamError, Status: 529})
	c3 := newCall("ws", "s", 3, turn1...) // the client retries with the same messages
	h.Before(bg, c3)
	if got := ringOf(t, c3); len(got) != 1 || got[0].Index != 1 {
		t.Fatalf("ring after a failed call = %+v, want only call 1", got)
	}
}

// A late After with no content (a call answered without a stream) keeps what the catch-up found, and takes
// its token counts.
func TestLateEmptyAfterKeepsTheCaughtUpTurn(t *testing.T) {
	h := history.New(history.Options{})
	user := tollgate.Message{Role: tollgate.RoleUser, Text: "task"}
	run(t, h, newCall("ws", "s", 1, user), 1, 1, toolDelta(0, "grep", `{"q":1}`))
	c2 := newCall("ws", "s", 2, user)
	h.Before(bg, c2)
	msgs := []tollgate.Message{user, asst("", tc("1", "grep", `{"q":1}`)), {Role: tollgate.RoleTool, ToolCallID: "1", Text: "r"},
		asst("", tc("2", "grep", `{"q":2}`)), {Role: tollgate.RoleTool, ToolCallID: "2", Text: "r"}}
	c3 := newCall("ws", "s", 3, msgs...)
	h.Before(bg, c3) // catches index 2 up
	h.After(bg, c2, tollgate.Metric{Finish: tollgate.FinishOK, Status: 200, Usage: tollgate.Usage{InputTokens: 42}})
	h.After(bg, c3, tollgate.Metric{Finish: tollgate.FinishUpstreamError, Status: 500})
	c4 := newCall("ws", "s", 4, msgs...)
	h.Before(bg, c4)
	got := ringOf(t, c4)
	if len(got) != 2 || got[1].Index != 2 || len(got[1].Tools) != 1 || got[1].Input != 42 {
		t.Fatalf("ring = %+v", got)
	}
}
