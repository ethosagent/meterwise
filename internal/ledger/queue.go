package ledger

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// Writer is what the batcher needs from the store; *store.Store implements it and tests fake it.
type Writer interface {
	WriteCalls(ctx context.Context, recs []contract.CallRecord) (store.WriteResult, error)
	CloseSessions(ctx context.Context, closes []contract.SessionClose) (int, error)
	// UnreadableToolCalls counts stored tool_calls values the store could not read; UnexpectedStateErrors
	// counts 55000 errors whose wording the retry policy does not recognise. Both go to /statusz.
	UnreadableToolCalls() int64
	UnexpectedStateErrors() int64
}

// Options configures the hook. Zero values take the documented defaults.
type Options struct {
	Writer Writer
	Pricer contract.Pricer
	// PrivacyMode is the global PRIVACY_MODE. nil means ON (fail safe): message text is stored only when
	// the caller passes a pointer to false, and then only for workspaces that opt in.
	PrivacyMode   *bool
	QueueSize     int           // default 10000
	BatchSize     int           // default 500
	FlushInterval time.Duration // default 250 ms
	Retries       int           // attempts per batch before it is given up on, for a failure other than an unreachable database; default 3
	RetryBackoff  time.Duration // default 200 ms: times the attempt number, or doubled per attempt while the database is unreachable
	// RetryWindow is how long a batch or session close is retried while the database is unreachable
	// (a restart or failover); default 30 s. The batch stays queued meanwhile, bounded by QueueSize.
	RetryWindow time.Duration
	// RetryBackoffMax caps the doubled backoff; default 5 s.
	RetryBackoffMax time.Duration
	Log             *slog.Logger
	Now             func() time.Time
}

func (o Options) withDefaults() (Options, error) {
	if o.Writer == nil || o.Pricer == nil {
		return o, errors.New("ledger: Writer and Pricer are required")
	}
	if o.PrivacyMode == nil {
		on := true
		o.PrivacyMode = &on
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
	if o.RetryWindow <= 0 {
		o.RetryWindow = 30 * time.Second
	}
	if o.RetryBackoffMax <= 0 {
		o.RetryBackoffMax = 5 * time.Second
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
	abort  context.CancelFunc // ends the batcher's writes when Close's context expires

	enqueued, dropped, flushed, duplicates, lost, lostCloses, skipped, wrongOrg, unpriced, sessionsClosed, flushErrors atomic.Int64

	errMu     sync.Mutex
	refused   map[int]int64 // calls refused before the upstream, by HTTP status
	lastErr   string
	lastFlush time.Time
}

func newQueue(o Options) *queue {
	return &queue{o: o, ch: make(chan item, o.QueueSize), stop: make(chan struct{}), done: make(chan struct{})}
}

// start runs the batcher. ctx is detached from the caller's cancellation: a flush must outlive it until Close.
func (q *queue) start(ctx context.Context) {
	ctx, q.abort = context.WithCancel(context.WithoutCancel(ctx))
	go q.run(ctx)
}

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

// refuse counts a call refused before it reached the upstream.
func (q *queue) refuse(status int) {
	q.errMu.Lock()
	if q.refused == nil {
		q.refused = map[int]int64{}
	}
	q.refused[status]++
	q.errMu.Unlock()
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
	}
	// Out of time: stop retrying. The batcher counts what is in its hands and still queued as lost
	// (every write now fails at once), so Status() stays honest; the wait for it is short and bounded.
	q.abort()
	select {
	case <-q.done:
	case <-time.After(5 * time.Second):
	}
	return ctx.Err()
}

func (q *queue) run(ctx context.Context) {
	defer close(q.done)
	tick := time.NewTicker(q.o.FlushInterval)
	defer tick.Stop()
	buf := make([]item, 0, q.o.BatchSize)
	for {
		select {
		case it := <-q.ch:
			buf = q.add(ctx, buf, it, false)
		case <-tick.C:
			if len(buf) > 0 {
				q.flush(ctx, buf, false)
				buf = buf[:0]
			}
		case <-q.stop:
			q.drain(ctx, buf)
			return
		}
	}
}

// add appends it and flushes a full batch.
func (q *queue) add(ctx context.Context, buf []item, it item, final bool) []item {
	buf = append(buf, it)
	if len(buf) >= q.o.BatchSize {
		q.flush(ctx, buf, final)
		buf = buf[:0]
	}
	return buf
}

// drain writes everything still queued; close() holds the write lock before stop, so nothing more
// can be enqueued.
func (q *queue) drain(ctx context.Context, buf []item) {
	for {
		select {
		case it := <-q.ch:
			buf = q.add(ctx, buf, it, true)
		default:
			if len(buf) > 0 {
				q.flush(ctx, buf, true)
			}
			return
		}
	}
}

// flush writes the calls of buf in one transaction, then applies its session closes. A failed batch
// is retried with backoff: for the retry window with doubling backoff while the database is
// unreachable, otherwise Retries times (not when shutting down). One that keeps failing for a reason
// other than an unreachable database is retried record by record, each with the same small retry, so
// a single poison record cannot sink the rest.
func (q *queue) flush(parent context.Context, buf []item, final bool) {
	var calls []contract.CallRecord
	var closes []contract.SessionClose
	for _, it := range buf {
		if it.call != nil {
			calls = append(calls, *it.call)
		} else {
			closes = append(closes, contract.SessionClose{WorkspaceID: it.closeWS, SessionID: it.closeID, At: it.closeAt})
		}
	}
	// one retry budget for the whole flush: an outage burns it once, not once per operation
	deadline := time.Now().Add(q.o.RetryWindow)
	ctx, cancel := context.WithTimeout(parent, q.o.RetryWindow+30*time.Second)
	defer cancel()
	if len(calls) > 0 {
		q.writeCalls(ctx, calls, final, deadline)
	}
	if len(closes) > 0 {
		q.closeSessions(ctx, closes, final, deadline)
	}
}

// closeSessions applies the session closes of a flush in one statement with the retry policy of a call
// batch. Closes that still fail are counted in lost_closes: those sessions stay open until a later call
// or close touches them.
func (q *queue) closeSessions(ctx context.Context, closes []contract.SessionClose, final bool, deadline time.Time) {
	for attempt := 1; ; attempt++ {
		n, err := q.o.Writer.CloseSessions(ctx, closes)
		if err == nil {
			q.sessionsClosed.Add(int64(n))
			q.ok()
			return
		}
		q.fail(ctx, err)
		if !q.again(ctx, attempt, deadline, err, final) {
			break
		}
	}
	q.lostCloses.Add(int64(len(closes)))
}

// again sleeps the backoff of a failed attempt and reports whether to try once more. While the database
// is unreachable the backoff doubles up to RetryBackoffMax and the tries last until deadline, also
// during the final flush (Close's context ends them); any other failure gets Retries attempts with a
// linear backoff, none during the final flush. A cancelled ctx never retries.
func (q *queue) again(ctx context.Context, attempt int, deadline time.Time, err error, final bool) bool {
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
		if time.Now().Add(d).After(deadline) {
			return false
		}
	} else {
		if final || attempt >= q.o.Retries {
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

func (q *queue) writeCalls(ctx context.Context, calls []contract.CallRecord, final bool, deadline time.Time) {
	err := q.tryWrite(ctx, calls, final, deadline)
	if err == nil {
		q.ok()
		return
	}
	if len(calls) > 1 && !store.IsTransient(err) && ctx.Err() == nil {
		for _, c := range calls { // isolate the record the database refuses
			if q.tryWrite(ctx, []contract.CallRecord{c}, final, deadline) != nil {
				q.lost.Add(1)
			}
		}
		return
	}
	q.lost.Add(int64(len(calls)))
}

// tryWrite writes recs, retrying as again allows; it returns the last error, or nil once written.
func (q *queue) tryWrite(ctx context.Context, recs []contract.CallRecord, final bool, deadline time.Time) error {
	for attempt := 1; ; attempt++ {
		res, err := q.o.Writer.WriteCalls(ctx, recs)
		if err == nil {
			q.flushed.Add(int64(res.Inserted))
			q.duplicates.Add(int64(res.Duplicates))
			return nil
		}
		q.fail(ctx, err)
		if !q.again(ctx, attempt, deadline, err, final) {
			return err
		}
	}
}

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

func (q *queue) status() map[string]any {
	q.errMu.Lock()
	lastErr, lastFlush := q.lastErr, q.lastFlush
	refused := make(map[string]int64, len(q.refused))
	for status, n := range q.refused {
		refused[strconv.Itoa(status)] = n
	}
	q.errMu.Unlock()
	st := map[string]any{
		"queue_depth":          len(q.ch),
		"queue_capacity":       cap(q.ch),
		"enqueued":             q.enqueued.Load(),
		"dropped":              q.dropped.Load(),
		"flushed":              q.flushed.Load(),
		"duplicates":           q.duplicates.Load(),
		"lost":                 q.lost.Load(),
		"lost_closes":          q.lostCloses.Load(),
		"skipped_wrong_org":    q.wrongOrg.Load(),
		"skipped_unattributed": q.skipped.Load(),
		"unpriced_calls":       q.unpriced.Load(),
		"sessions_closed":      q.sessionsClosed.Load(),
		"flush_errors":         q.flushErrors.Load(),
		"refused_by_status":    refused,
		"last_flush_error":     lastErr,
	}
	if !lastFlush.IsZero() {
		st["last_flush_at"] = lastFlush.UTC().Format(time.RFC3339)
	}
	return st
}
