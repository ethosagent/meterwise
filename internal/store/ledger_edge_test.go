package store_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

// A session's started_at is its sort key in the keyset pagination: it is fixed by the first insert. A
// call that arrives later with an earlier start (a late batch) must not move the session across a
// cursor that was already issued.
func TestSessionStartedAtNeverMovesAfterTheFirstInsert(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{rec(ws, "late", "s1", 2, base.Add(time.Minute), "ok", true, mu(10))}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{rec(ws, "early", "s1", 1, base, "ok", true, mu(10))}); err != nil {
		t.Fatal(err)
	}
	sess, calls, _, err := s.Session(ctx, ws, "s1")
	if err != nil || sess.Calls != 2 || len(calls) != 2 {
		t.Fatalf("%+v %d %v", sess, len(calls), err)
	}
	if !sess.StartedAt.Equal(base.Add(time.Minute)) {
		t.Fatalf("started_at moved to %v after a later batch carried an earlier call", sess.StartedAt)
	}
}

// A tool_calls value that is not a JSON array of names reads back as no tool calls, but it is counted
// on every read and logged once, never swallowed.
func TestUnreadableToolCallsAreCountedAndLoggedOnce(t *testing.T) {
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(context.Background(), dsn); err != nil {
		t.Fatal(err)
	}
	s, ctx := mustOpen(t, dsn), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{rec(ws, "c1", "s1", 1, base, "ok", true, mu(10))}); err != nil {
		t.Fatal(err)
	}
	adminExec(t, dsn, `UPDATE calls SET tool_calls = '{"not":"an array"}'::jsonb WHERE id = 'c1'`)
	var logs bytes.Buffer
	s.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	var global bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&global, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	for range 2 {
		_, calls, _, err := s.Session(ctx, ws, "s1")
		if err != nil || len(calls) != 1 || len(calls[0].ToolCalls) != 0 {
			t.Fatalf("%+v %v", calls, err)
		}
	}
	if n := s.UnreadableToolCalls(); n != 2 {
		t.Fatalf("counter = %d, want 2", n)
	}
	if n := strings.Count(logs.String(), "tool_calls"); n != 1 {
		t.Fatalf("logged %d times, want once:\n%s", n, logs.String())
	}
	if global.Len() != 0 {
		t.Fatalf("the store wrote to slog.Default: %s", global.String())
	}
}

// A call with no tool calls reads back as an empty list, never nil (which would marshal as null).
func TestSessionCallsNeverHaveNilToolCalls(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	r := rec(ws, "c1", "s1", 1, base, "ok", true, nil)
	r.ToolCalls = nil
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{r}); err != nil {
		t.Fatal(err)
	}
	_, calls, _, err := s.Session(ctx, ws, "s1")
	if err != nil || len(calls) != 1 || calls[0].ToolCalls == nil || len(calls[0].ToolCalls) != 0 {
		t.Fatalf("calls = %+v, %v; want one call with a non-nil empty ToolCalls", calls, err)
	}
}

// The session read is capped at 5000 calls; the store, not the API, says whether it cut anything.
func TestSessionReportsTruncation(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	for _, tc := range []struct {
		n    int
		want bool
	}{{5000, false}, {5001, true}} {
		sid := fmt.Sprintf("s%d", tc.n)
		batch := make([]contract.CallRecord, tc.n)
		for i := range batch {
			batch[i] = rec(ws, fmt.Sprintf("%s-%d", sid, i), sid, i+1, base.Add(time.Duration(i)*time.Millisecond), "ok", true, nil)
		}
		if _, err := s.WriteCalls(ctx, batch); err != nil {
			t.Fatal(err)
		}
		_, calls, truncated, err := s.Session(ctx, ws, sid)
		if err != nil || len(calls) != min(tc.n, 5000) || truncated != tc.want {
			t.Errorf("%d calls: got %d calls truncated=%v err=%v, want truncated=%v", tc.n, len(calls), truncated, err, tc.want)
		}
	}
}
