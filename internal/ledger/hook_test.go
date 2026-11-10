package ledger_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/ledger"
	"github.com/ethosagent/meterwise/internal/savings"
)

func byIndex(recs []contract.CallRecord) map[int]contract.CallRecord {
	m := map[int]contract.CallRecord{}
	for _, r := range recs {
		m[r.CallIndex] = r
	}
	return m
}

func TestEveryCompletedCallIsOneRowWithTokensCostAndTools(t *testing.T) {
	w := &fakeWriter{}
	h := newHook(t, w, nil)
	fx := []string{"openai/chat-stream-text", "openai/chat", "openai/chat-stream-tools", "anthropic/messages-stream-tool-use", "anthropic/messages", "openai/error-500"}
	rec := drive(t, h, nil, fx...)
	if len(rec.Replies) != len(fx) {
		t.Fatalf("replies = %d", len(rec.Replies))
	}
	got := w.snapshot()
	if len(got) != len(fx) {
		t.Fatalf("calls rows = %d, want exactly one per call (%d)", len(got), len(fx))
	}
	ids := map[string]bool{}
	for _, r := range got {
		if ids[r.ID] {
			t.Errorf("duplicate id %s", r.ID)
		}
		ids[r.ID] = true
		if r.WorkspaceID != wsID || r.OrgID != orgID || r.SessionID != "testkit-session" || r.SessionSource != contract.SessionSourceHeader {
			t.Errorf("attribution wrong: %+v", r)
		}
	}
	by := byIndex(got)
	text := by[1]
	// 12 input, 2 output on gpt-4.1-mini at 0.40 / 1.60 per million = 4.8 + 3.2 micro-dollars
	if text.Usage.Input != 12 || text.Usage.Output != 2 || text.Cost == nil || *text.Cost != 8 || text.Model != "gpt-4.1-mini" || !text.Stream || text.Status != 200 || text.Finish != "ok" || text.Wire != "openai.chat" {
		t.Errorf("stream text call = %+v cost %v", text, text.Cost)
	}
	if text.TTFBMs < 0 || text.DurationMs < text.TTFBMs || text.Client != "Go-http-client/1.1" {
		t.Errorf("timing/client: %+v", text)
	}
	if !text.LastHadText || len(text.ToolCalls) != 0 {
		t.Errorf("text stream: hadText=%v tools=%v", text.LastHadText, text.ToolCalls)
	}
	if plain := by[2]; plain.Stream || !plain.LastHadText || len(plain.ToolCalls) != 0 || plain.Cost == nil {
		t.Errorf("non-streamed call = %+v", plain)
	}
	if tools := by[3]; !slices.Equal(tools.ToolCalls, []string{"get_weather"}) || tools.LastHadText || tools.Usage.Input != 40 || tools.Usage.Output != 11 {
		t.Errorf("openai tool stream = %+v", tools)
	}
	if an := by[4]; !slices.Equal(an.ToolCalls, []string{"get_weather"}) || an.Provider != "anthropic" || an.Wire != "anthropic.messages" || an.Cost == nil {
		t.Errorf("anthropic tool stream = %+v", an)
	}
	if bad := by[6]; bad.Status != 500 || bad.Finish != "upstream_error" || bad.LastHadText {
		t.Errorf("upstream error call = %+v", bad)
	}
	if st := h.Status(); st["dropped"] != int64(0) || st["flushed"] != int64(len(fx)) || st["last_flush_error"] != "" {
		t.Errorf("status = %v", st)
	}
	if w.closed[0] != wsID+"/testkit-session" {
		t.Errorf("session not closed on quiet: %v", w.closed)
	}
}

func TestUnknownModelIsNullCostAndCounted(t *testing.T) {
	w := &fakeWriter{}
	h := newHook(t, w, func(o *ledger.Options) { o.Pricer = savings.NewTablePricer(nil) })
	drive(t, h, nil, "openai/chat-stream-text", "openai/chat")
	got := w.snapshot()
	if len(got) != 2 {
		t.Fatalf("rows = %d", len(got))
	}
	for _, r := range got {
		if r.Cost != nil {
			t.Errorf("unpriced model got cost %d, want NULL", *r.Cost)
		}
	}
	if h.Status()["unpriced_calls"] != int64(2) {
		t.Errorf("unpriced not counted: %v", h.Status())
	}
}

func TestBeforeStampsWithoutIO(t *testing.T) {
	h := newHook(t, &fakeWriter{}, nil)
	c := directCall("c1", "hi", nil, 3)
	start := time.Now()
	resp, err := h.Before(context.Background(), c)
	if err != nil || resp != nil {
		t.Fatalf("Before = %v, %v", resp, err)
	}
	if took := time.Since(start); took > time.Millisecond {
		t.Errorf("Before took %v, budget 1 ms", took)
	}
	if len(c.Meta) != 1 { // one entry: the call state (start, index, stream facts)
		t.Errorf("meta = %v", c.Meta)
	}
	m := metricFor(c)
	m.Start = time.Time{} // the hook falls back to when the call reached it, and to its session position
	w := &fakeWriter{}
	h2 := newHook(t, w, nil)
	if _, err := h2.Before(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	h2.After(context.Background(), c, m)
	if err := h2.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := w.snapshot(); len(got) != 1 || got[0].CallIndex != 3 || got[0].StartedAt.IsZero() || time.Since(got[0].StartedAt) > time.Minute {
		t.Errorf("record = %+v, want call index 3 and the time Before saw", got)
	}
	if h.Name() != "ledger" {
		t.Errorf("name = %q", h.Name())
	}
}

func TestQueueNeverBlocksAndDropsAreCounted(t *testing.T) {
	w := &fakeWriter{delay: 300 * time.Millisecond} // a database that is stuck
	h := newHook(t, w, func(o *ledger.Options) { o.QueueSize = 5; o.BatchSize = 2; o.FlushInterval = time.Millisecond })
	const n = 200
	start := time.Now()
	for i := range n {
		c := directCall("call-"+string(rune('A'+i%26))+strings.Repeat("x", i), "hi", nil, i+1)
		complete(t, h, c)
	}
	if took := time.Since(start); took > 100*time.Millisecond {
		t.Fatalf("%d After calls against a stuck writer took %v: the request path blocked", n, took)
	}
	st := h.Status()
	dropped, _ := st["dropped"].(int64)
	if dropped < n-20 || st["queue_capacity"] != 5 {
		t.Fatalf("expected most calls dropped and counted, status = %v", st)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if st := h.Status(); st["enqueued"].(int64)+st["dropped"].(int64) != n {
		t.Fatalf("enqueued+dropped != %d: %v", n, st)
	}
	late := directCall("after-close", "hi", nil, 1)
	complete(t, h, late) // after Close: dropped and counted, never a panic
	if h.Status()["dropped"].(int64) <= dropped {
		t.Error("a record after Close was not counted as dropped")
	}
}

func TestCloseFlushesWhatIsQueued(t *testing.T) {
	w := &fakeWriter{}
	h := newHook(t, w, func(o *ledger.Options) { o.FlushInterval = time.Hour })
	for i := range 7 {
		complete(t, h, directCall("flush-"+string(rune('a'+i)), "hi", nil, i+1))
	}
	if len(w.snapshot()) != 0 {
		t.Fatal("nothing should be written before the interval or Close")
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.snapshot()) != 7 {
		t.Fatalf("Close flushed %d rows, want 7", len(w.snapshot()))
	}
}

func TestBatchesFlushAtBatchSize(t *testing.T) {
	w := &fakeWriter{}
	h := newHook(t, w, func(o *ledger.Options) { o.BatchSize = 4; o.FlushInterval = time.Hour })
	for i := range 9 {
		complete(t, h, directCall("b-"+string(rune('a'+i)), "hi", nil, i+1))
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !slices.Equal(w.batches, []int{4, 4, 1}) {
		t.Fatalf("batches = %v, want [4 4 1]", w.batches)
	}
}

func TestFailedWritesRetryThenIsolateAPoisonRecord(t *testing.T) {
	poison := errors.New("store: refused")
	w := &fakeWriter{fail: func(recs []contract.CallRecord) error {
		for _, r := range recs {
			if r.ID == "poison" {
				return poison
			}
		}
		return nil
	}}
	h := newHook(t, w, func(o *ledger.Options) { o.RetryBackoff = time.Millisecond; o.FlushInterval = time.Hour })
	for _, id := range []string{"ok-1", "poison", "ok-2"} {
		complete(t, h, directCall(id, "hi", nil, 1))
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := w.snapshot()
	if len(got) != 2 || got[0].ID != "ok-1" || got[1].ID != "ok-2" {
		t.Fatalf("rows = %+v, want the two good records", got)
	}
	st := h.Status()
	if st["lost"] != int64(1) || st["flush_errors"].(int64) < 2 || st["last_flush_error"] == "" {
		t.Fatalf("status = %v", st)
	}
}

func TestUnattributableCallsAreSkippedNotWritten(t *testing.T) {
	w := &fakeWriter{}
	h := newHook(t, w, nil)
	c := directCall("dev-1", "hi", nil, 1)
	c.Tenant = tollgate.Tenant{ID: "default"} // the dev NoAuth tenant: no workspace, no org
	complete(t, h, c)
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.snapshot()) != 0 || h.Status()["skipped_unattributed"] != int64(1) {
		t.Fatalf("rows %d status %v", len(w.snapshot()), h.Status())
	}
}

func TestHostileFieldsCannotPoisonABatch(t *testing.T) {
	w := &fakeWriter{}
	h := newHook(t, w, nil)
	c := directCall("hostile", "hi", nil, 1)
	c.Session.ID = ""
	c.Model = "gpt-4o\x00" + strings.Repeat("é", 500)
	m := metricFor(c)
	m.Model = c.Model
	m.Usage.InputTokens = -5
	if _, err := h.Before(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	h.OnEvent(context.Background(), c, tollgate.Event{Kind: tollgate.EventToolCallDelta, ToolCall: &tollgate.ToolCallDelta{Name: "bad\x00name"}})
	h.After(context.Background(), c, m)
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := w.snapshot()[0]
	if strings.ContainsRune(r.Model, 0) || len(r.Model) > 200 || r.Usage.Input != 0 || r.SessionID != "call-hostile" || r.ToolCalls[0] != "badname" {
		t.Fatalf("record not sanitised: %+v", r)
	}
}

func TestTransientFailureIsRetriedWithoutLoss(t *testing.T) {
	var attempts int
	w := &fakeWriter{fail: func([]contract.CallRecord) error {
		attempts++
		if attempts <= 2 {
			return errors.New("store: database unreachable")
		}
		return nil
	}}
	h := newHook(t, w, func(o *ledger.Options) { o.RetryBackoff = time.Millisecond; o.FlushInterval = time.Millisecond })
	complete(t, h, directCall("retry-1", "hi", nil, 1))
	deadline := time.Now().Add(5 * time.Second)
	for len(w.snapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.snapshot()) != 1 || h.Status()["lost"] != int64(0) || h.Status()["last_flush_error"] != "" {
		t.Fatalf("rows %d status %v", len(w.snapshot()), h.Status())
	}
}
