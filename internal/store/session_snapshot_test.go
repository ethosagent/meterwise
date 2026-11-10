package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

// SessionDetail reads the session, its calls and its events from one snapshot. The events query is held
// back by an exclusive lock while another transaction changes the event and commits; a read at READ
// COMMITTED would then see the new value next to the calls it read before, a REPEATABLE READ snapshot sees
// the world as it was when the read began.
func TestSessionDetailReadsOneSnapshot(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	c := rec(ws, "c1", "s1", 1, base, "ok", true, nil)
	c.Verdicts = []contract.PricedVerdict{verdict(contract.MonitorRepeatedRead, contract.ModeNote, contract.ConfidenceLikely, 1, 1)}
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{c}); err != nil {
		t.Fatal(err)
	}
	holder, err := s.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Rollback(context.Background()) })
	if _, err := holder.Exec(ctx, `LOCK TABLE monitor_events IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		events []contract.MonitorEventRecord
		err    error
	}
	done := make(chan result, 1)
	go func() {
		_, _, _, evs, err := s.SessionDetail(ctx, ws, "s1")
		done <- result{evs, err}
	}()
	// wait until the reader is stuck on the lock, i.e. it has read the session and the calls already
	var waiting int
	for deadline := time.Now().Add(10 * time.Second); waiting == 0 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if err := s.Pool().QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query LIKE '%monitor_events%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
	}
	if waiting == 0 {
		t.Fatal("the reader never reached the events query")
	}
	if _, err := holder.Exec(ctx, `UPDATE monitor_events SET est_saved_tokens = 999 WHERE call_id = 'c1'`); err != nil {
		t.Fatal(err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.err != nil || len(r.events) != 1 {
			t.Fatalf("%+v", r)
		}
		if got := r.events[0].EstSavedToken; got != 1 {
			t.Fatalf("est_saved_tokens = %d: the events were read after the session began, not from its snapshot", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SessionDetail never returned")
	}
}
