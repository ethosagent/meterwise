package ledger

import (
	"context"
	"log/slog"
	"strconv"
	"sync/atomic"

	"github.com/ethosagent/meterwise/internal/contract"
)

func (q *queue) fail(ctx context.Context, err error) {
	q.flushErrors.Add(1)
	q.errMu.Lock()
	q.lastErr = err.Error()
	q.errMu.Unlock()
	q.o.Log.LogAttrs(ctx, slog.LevelError, "ledger: flush failed", slog.String("err", err.Error()))
}

func (q *queue) ok() {
	q.errMu.Lock()
	q.lastErr = ""
	q.lastFlush = q.o.Now()
	q.errMu.Unlock()
}

// counters is the table status() prints: one row per atomic counter, in its /statusz name.
func (q *queue) counters() []struct {
	name string
	n    *atomic.Int64
} {
	return []struct {
		name string
		n    *atomic.Int64
	}{
		{"enqueued", &q.enqueued}, {"dropped", &q.dropped}, {"flushed", &q.flushed}, {"duplicates", &q.duplicates},
		{"lost", &q.lost}, {"lost_closes", &q.lostCloses}, {"skipped_wrong_org", &q.wrongOrg},
		{"skipped_unattributed", &q.skipped}, {"unpriced_calls", &q.unpriced}, {"sessions_closed", &q.sessionsClosed},
		{"flush_errors", &q.flushErrors}, {"blocks_downgraded", &q.blocksDowngraded}, {"invalid_verdicts", &q.invalidVerdicts},
	}
}

func (q *queue) status() map[string]any {
	q.errMu.Lock()
	lastErr, lastFlush := q.lastErr, q.lastFlush
	q.errMu.Unlock()
	refused := map[string]int64{}
	for status := range q.refused {
		if n := q.refused[status].Load(); n > 0 {
			refused[strconv.Itoa(status)] = n
		}
	}
	st := map[string]any{
		"queue_depth":       len(q.ch),
		"queue_capacity":    cap(q.ch),
		"refused_by_status": refused,
		"last_flush_error":  lastErr,
	}
	for _, c := range q.counters() {
		st[c.name] = c.n.Load()
	}
	if !lastFlush.IsZero() {
		st["last_flush_at"] = contract.RFC3339(lastFlush)
	}
	return st
}
