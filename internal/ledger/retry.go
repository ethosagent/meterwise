package ledger

import (
	"context"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// retryPlan is the retry budget of one flush: final is true while shutting down, deadline ends the
// retries of an unreachable database. One plan covers the whole flush, so an outage burns it once.
type retryPlan struct {
	final    bool
	deadline time.Time
}

// closeSessions applies the session closes of a flush in one statement. Closes that still fail are
// counted in lost_closes: those sessions stay open until a later call or close touches them.
func (q *queue) closeSessions(ctx context.Context, closes []contract.SessionClose, plan retryPlan) {
	var closed []store.ClosedSession
	err := q.retry(ctx, plan, func() error {
		n, rows, err := q.closeOnce(ctx, closes)
		if err == nil {
			q.sessionsClosed.Add(int64(n))
			closed = rows
		}
		return err
	})
	if err != nil {
		q.lostCloses.Add(int64(len(closes)))
		return
	}
	q.ok()
	q.notifyClosed(ctx, closed)
}

// closeOnce applies the closes. With an OnClosed callback and a Writer that reports what it closed, it also returns
// the sessions this statement really closed.
func (q *queue) closeOnce(ctx context.Context, closes []contract.SessionClose) (int, []store.ClosedSession, error) {
	if cr, ok := q.o.Writer.(closeReturner); ok && q.onClosed.Load() != nil {
		rows, err := cr.CloseSessionsReturning(ctx, closes)
		return len(rows), rows, err
	}
	n, err := q.o.Writer.CloseSessions(ctx, closes)
	return n, nil, err
}

// retry runs op until it succeeds or again says to stop, counting and logging every failure; it
// returns the last error, or nil once op succeeded. The caller decides when a success is "ok".
func (q *queue) retry(ctx context.Context, plan retryPlan, op func() error) error {
	for attempt := 1; ; attempt++ {
		err := op()
		if err == nil {
			return nil
		}
		q.fail(ctx, err)
		if !q.again(ctx, attempt, plan, err) {
			return err
		}
	}
}

// again sleeps the backoff of a failed attempt and reports whether to try once more. While the database
// is unreachable the backoff doubles up to RetryBackoffMax and the tries last until the plan's deadline,
// also during the final flush (Close's context ends them); any other failure gets Retries attempts with
// a linear backoff, none during the final flush. A cancelled ctx never retries.
func (q *queue) again(ctx context.Context, attempt int, plan retryPlan, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var d time.Duration
	if store.IsTransient(err) {
		d = q.o.RetryBackoff
		for i := 1; i < attempt && d < q.o.RetryBackoffMax; i++ {
			d *= 2
		}
		d = min(d, q.o.RetryBackoffMax)
		if time.Now().Add(d).After(plan.deadline) {
			return false
		}
	} else {
		if plan.final || attempt >= q.o.Retries {
			return false
		}
		d = time.Duration(attempt) * q.o.RetryBackoff
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (q *queue) writeCalls(ctx context.Context, calls []contract.CallRecord, hits []store.MemoryHit, plan retryPlan) {
	err := q.tryWrite(ctx, calls, hits, plan)
	if err == nil {
		q.ok()
		return
	}
	if len(calls) > 1 && !store.IsTransient(err) && ctx.Err() == nil {
		for _, c := range calls { // isolate the record the database refuses
			if q.tryWrite(ctx, []contract.CallRecord{c}, hits, plan) != nil {
				q.lost.Add(1)
			}
		}
		return
	}
	q.lost.Add(int64(len(calls)))
}

// tryWrite writes recs, retrying as the plan allows; it returns the last error, or nil once written.
func (q *queue) tryWrite(ctx context.Context, recs []contract.CallRecord, hits []store.MemoryHit, plan retryPlan) error {
	return q.retry(ctx, plan, func() error {
		res, err := q.writeOnce(ctx, recs, hits)
		if err == nil {
			q.flushed.Add(int64(res.Inserted))
			q.duplicates.Add(int64(res.Duplicates))
		}
		return err
	})
}
