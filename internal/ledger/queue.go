package ledger

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// Writer is what the batcher needs from the store; *store.Store implements it and tests fake it.
type Writer interface {
	WriteCalls(ctx context.Context, recs []contract.CallRecord) (store.WriteResult, error)
	CloseSession(ctx context.Context, workspaceID, sessionID string, at time.Time) (bool, error)
}

// Options configures the hook. Zero values take the documented defaults.
type Options struct {
	Writer        Writer
	Pricer        contract.Pricer
	PrivacyMode   bool          // the global PRIVACY_MODE; true means no message text is ever stored
	QueueSize     int           // default 10000
	BatchSize     int           // default 500
	FlushInterval time.Duration // default 250 ms
	Retries       int           // attempts per batch before it is given up on; default 3
	RetryBackoff  time.Duration // default 200 ms, times the attempt number
	Log           *slog.Logger
	Now           func() time.Time
}

func (o Options) withDefaults() (Options, error) {
	if o.Writer == nil || o.Pricer == nil {
		return o, errors.New("ledger: Writer and Pricer are required")
	}
	if o.QueueSize <= 0 {
		o.QueueSize = 10_000
	}
	if o.BatchSize <= 0 {
		o.BatchSize = 500
	}
	if o.FlushInterval <= 0 {
		o.FlushInterval = 250 * time.Millisecond
	}
	if o.Retries <= 0 {
		o.Retries = 3
	}
	if o.RetryBackoff <= 0 {
		o.RetryBackoff = 200 * time.Millisecond
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o, nil
}

// item is one queue entry: a call to record, or a session to close.
type item struct {
	call    *contract.CallRecord
	closeWS string
	closeID string
	closeAt time.Time
}

type queue struct {
	o  Options
	ch chan item

	mu     sync.RWMutex // held for reading by enqueue, for writing by close
	closed bool
	stop   chan struct{}
	done   chan struct{}

	enqueued, dropped, flushed, duplicates, lost, skipped, unpriced, sessionsClosed, flushErrors atomic.Int64

	errMu     sync.Mutex
	lastErr   string
	lastFlush time.Time
}

func newQueue(o Options) *queue {
	return &queue{o: o, ch: make(chan item, o.QueueSize), stop: make(chan struct{}), done: make(chan struct{})}
}

func (q *queue) start() { go q.run() }

// enqueue never blocks: a full or closed queue drops the item and counts it.
func (q *queue) enqueue(it item) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.closed {
		q.dropped.Add(1)
		return
	}
	select {
	case q.ch <- it:
		q.enqueued.Add(1)
	default:
		q.dropped.Add(1)
	}
}

func (q *queue) close(ctx context.Context) error {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.stop)
	}
	q.mu.Unlock()
	select {
	case <-q.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *queue) run() {
	defer close(q.done)
	tick := time.NewTicker(q.o.FlushInterval)
	defer tick.Stop()
	buf := make([]item, 0, q.o.BatchSize)
	for {
		select {
		case it := <-q.ch:
			buf = append(buf, it)
			if len(buf) >= q.o.BatchSize {
				q.flush(buf, false)
				buf = buf[:0]
			}
		case <-tick.C:
			if len(buf) > 0 {
				q.flush(buf, false)
				buf = buf[:0]
			}
		case <-q.stop:
			for { // close() holds the write lock before stop, so nothing more is enqueued: drain what is there
				select {
				case it := <-q.ch:
					buf = append(buf, it)
					if len(buf) >= q.o.BatchSize {
						q.flush(buf, true)
						buf = buf[:0]
					}
				default:
					if len(buf) > 0 {
						q.flush(buf, true)
					}
					return
				}
			}
		}
	}
}

// flush writes the calls of buf in one transaction, then applies its session closes. A failed batch
// is retried with backoff (not when shutting down); one that keeps failing for a reason other than an
// unreachable database is retried record by record so a single poison record cannot sink the rest.
func (q *queue) flush(buf []item, final bool) {
	var calls []contract.CallRecord
	for _, it := range buf {
		if it.call != nil {
			calls = append(calls, *it.call)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if len(calls) > 0 {
		q.writeCalls(ctx, calls, final)
	}
	for _, it := range buf {
		if it.call != nil {
			continue
		}
		if ok, err := q.o.Writer.CloseSession(ctx, it.closeWS, it.closeID, it.closeAt); err != nil {
			q.fail(err)
		} else if ok {
			q.sessionsClosed.Add(1)
		}
	}
}

func (q *queue) writeCalls(ctx context.Context, calls []contract.CallRecord, final bool) {
	var err error
	for attempt := 1; attempt <= q.o.Retries; attempt++ {
		var res store.WriteResult
		if res, err = q.o.Writer.WriteCalls(ctx, calls); err == nil {
			q.flushed.Add(int64(res.Inserted))
			q.duplicates.Add(int64(res.Duplicates))
			q.ok()
			return
		}
		q.fail(err)
		if final || attempt == q.o.Retries {
			break
		}
		time.Sleep(time.Duration(attempt) * q.o.RetryBackoff)
	}
	if len(calls) > 1 && store.ClassOf(err) != store.ClassUnreachable {
		for _, c := range calls { // isolate the record the database refuses
			if res, e := q.o.Writer.WriteCalls(ctx, []contract.CallRecord{c}); e != nil {
				q.fail(e)
				q.lost.Add(1)
			} else {
				q.flushed.Add(int64(res.Inserted))
				q.duplicates.Add(int64(res.Duplicates))
			}
		}
		return
	}
	q.lost.Add(int64(len(calls)))
}

func (q *queue) fail(err error) {
	q.flushErrors.Add(1)
	q.errMu.Lock()
	q.lastErr = err.Error()
	q.errMu.Unlock()
	q.o.Log.LogAttrs(context.Background(), slog.LevelError, "ledger: flush failed", slog.String("err", err.Error()))
}

func (q *queue) ok() {
	q.errMu.Lock()
	q.lastErr = ""
	q.lastFlush = q.o.Now()
	q.errMu.Unlock()
}

func (q *queue) status() map[string]any {
	q.errMu.Lock()
	lastErr, lastFlush := q.lastErr, q.lastFlush
	q.errMu.Unlock()
	st := map[string]any{
		"queue_depth":          len(q.ch),
		"queue_capacity":       cap(q.ch),
		"enqueued":             q.enqueued.Load(),
		"dropped":              q.dropped.Load(),
		"flushed":              q.flushed.Load(),
		"duplicates":           q.duplicates.Load(),
		"lost":                 q.lost.Load(),
		"skipped_unattributed": q.skipped.Load(),
		"unpriced_calls":       q.unpriced.Load(),
		"sessions_closed":      q.sessionsClosed.Load(),
		"flush_errors":         q.flushErrors.Load(),
		"last_flush_error":     lastErr,
	}
	if !lastFlush.IsZero() {
		st["last_flush_at"] = lastFlush.UTC().Format(time.RFC3339)
	}
	return st
}
