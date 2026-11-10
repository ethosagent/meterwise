package ledger_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/ledger"
	"github.com/ethosagent/meterwise/internal/store"
)

func unreachable() error {
	return &store.Error{Class: store.ClassUnreachable, Message: "store: database unreachable"}
}

// A failover or restart of a few seconds must not lose what the queue can hold: the failed batch stays
// queued and is retried with capped exponential backoff (default window 30 s), session closes included.
func TestADatabaseOutageOfAFewSecondsLosesNothing(t *testing.T) {
	back := time.Now().Add(3 * time.Second)
	down := func() error {
		if time.Now().Before(back) {
			return unreachable()
		}
		return nil
	}
	w := &fakeWriter{fail: func([]contract.CallRecord) error { return down() }, failClose: down}
	h := newHook(t, w, nil) // default retry policy
	for _, id := range []string{"c1", "c2", "c3", "c4", "c5"} {
		complete(t, h, directCall(id, "hi", nil, 1))
	}
	for _, s := range []string{"s1", "s2", "s3"} {
		h.OnSessionClosed(context.Background(), tollgate.Session{ID: s, TenantID: wsID})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	for len(w.snapshot()) < 5 || len(closedList(w)) < 3 {
		if ctx.Err() != nil {
			t.Fatalf("rows %d closes %d status %v", len(w.snapshot()), len(closedList(w)), h.Status())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	st := h.Status()
	if st["lost"] != int64(0) || st["lost_closes"] != int64(0) || st["dropped"] != int64(0) || st["flushed"] != int64(5) || st["sessions_closed"] != int64(3) {
		t.Fatalf("status = %v", st)
	}
}

// An outage that outlasts the retry window loses the batch, and says so.
func TestAnOutagePastTheRetryWindowIsCountedAsLost(t *testing.T) {
	w := &fakeWriter{fail: func([]contract.CallRecord) error { return unreachable() }}
	h := newHook(t, w, func(o *ledger.Options) {
		o.RetryWindow, o.RetryBackoff, o.RetryBackoffMax = 150*time.Millisecond, 10*time.Millisecond, 40*time.Millisecond
	})
	complete(t, h, directCall("c1", "hi", nil, 1))
	deadline := time.Now().Add(5 * time.Second)
	for h.Status()["lost"] != int64(1) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := h.Status(); st["lost"] != int64(1) || st["flush_errors"].(int64) < 3 {
		t.Fatalf("status = %v", st)
	}
}

// If Close's context ends before the queue is written, what was still queued is counted as lost.
func TestCloseDeadlineCountsWhatIsStillQueuedAsLost(t *testing.T) {
	w := &fakeWriter{fail: func([]contract.CallRecord) error { return unreachable() }, failClose: unreachable}
	h := newHook(t, w, nil)
	for _, id := range []string{"c1", "c2", "c3", "c4", "c5"} {
		complete(t, h, directCall(id, "hi", nil, 1))
	}
	h.OnSessionClosed(context.Background(), tollgate.Session{ID: "s1", TenantID: wsID})
	h.OnSessionClosed(context.Background(), tollgate.Session{ID: "s2", TenantID: wsID})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := h.Close(ctx); err == nil {
		t.Fatal("Close returned nil although the database never came back")
	}
	st := h.Status()
	if st["lost"] != int64(5) || st["lost_closes"] != int64(2) || st["queue_depth"] != 0 {
		t.Fatalf("status = %v, want 5 calls and 2 closes lost and nothing left queued", st)
	}
}

// Isolating a poison record gives every record the same small retry a batch gets.
func TestIsolatedRecordsAreRetriedLikeABatch(t *testing.T) {
	var flakyTries atomic.Int32
	w := &fakeWriter{fail: func(recs []contract.CallRecord) error {
		if len(recs) > 1 {
			return errors.New("store: refused")
		}
		if recs[0].ID == "flaky" && flakyTries.Add(1) == 1 {
			return errors.New("store: blip")
		}
		return nil
	}}
	h := newHook(t, w, func(o *ledger.Options) {
		o.BatchSize, o.FlushInterval, o.RetryBackoff = 2, time.Hour, time.Millisecond
	})
	complete(t, h, directCall("ok-1", "hi", nil, 1))
	complete(t, h, directCall("flaky", "hi", nil, 2))
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := w.snapshot(); len(got) != 2 {
		t.Fatalf("rows = %+v, want both records", got)
	}
	if st := h.Status(); st["lost"] != int64(0) {
		t.Fatalf("status = %v", st)
	}
}
