package ledger

import (
	"cmp"
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
	CloseSessions(ctx context.Context, closes []contract.SessionClose) (int, error)
	// UnreadableToolCalls counts stored tool_calls values the store could not read; UnexpectedStateErrors
	// counts 55000 errors whose wording the retry policy does not recognise. Both go to /statusz.
	UnreadableToolCalls() int64
	UnreadableEvidence() int64 // monitor_events.evidence values that did not decode
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
	// Estimator prices what a verdict saves (savings.Estimate when the host wires it); nil finds nothing:
	// zero tokens, confidence none, so verdicts are recorded without a saving.
	Estimator Estimator
	Log       *slog.Logger
	Now       func() time.Time
}

// Defaults of the zero Options values.
const (
	defaultQueueSize       = 10_000
	defaultBatchSize       = 500
	defaultFlushInterval   = 250 * time.Millisecond
	defaultRetries         = 3
	defaultRetryBackoff    = 200 * time.Millisecond
	defaultRetryWindow     = 30 * time.Second
	defaultRetryBackoffMax = 5 * time.Second
)

func (o Options) withDefaults() (Options, error) {
	if o.Writer == nil || o.Pricer == nil {
		return o, errors.New("ledger: Writer and Pricer are required")
	}
	if o.PrivacyMode == nil {
		on := true
		o.PrivacyMode = &on
	}
	// cmp.Or takes the first non-zero value; max(v, 0) makes a negative setting mean "default" too.
	o.QueueSize = cmp.Or(max(o.QueueSize, 0), defaultQueueSize)
	o.BatchSize = cmp.Or(max(o.BatchSize, 0), defaultBatchSize)
	o.FlushInterval = cmp.Or(max(o.FlushInterval, 0), defaultFlushInterval)
	o.Retries = cmp.Or(max(o.Retries, 0), defaultRetries)
	o.RetryBackoff = cmp.Or(max(o.RetryBackoff, 0), defaultRetryBackoff)
	o.RetryWindow = cmp.Or(max(o.RetryWindow, 0), defaultRetryWindow)
	o.RetryBackoffMax = cmp.Or(max(o.RetryBackoffMax, 0), defaultRetryBackoffMax)
	if o.Estimator == nil {
		o.Estimator = zeroEstimate
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
	// blocksDowngraded counts block verdicts on a call that already had usage (the upstream was reached first);
	// invalidVerdicts counts verdicts dropped for a name, mode or action outside the fixed sets.
	blocksDowngraded, invalidVerdicts atomic.Int64
	// trimUnpriced and routerUnpriced count saver savings left at 0 because a model had no price; invalidSaverMeta
	// counts trim or router results dropped for a value outside the fixed sets (they would break the table CHECKs).
	trimUnpriced, routerUnpriced, invalidSaverMeta atomic.Int64

	// refused counts calls refused before the upstream, indexed by HTTP status (a status outside 0..599 is 0).
	refused [maxStatus]atomic.Int64

	errMu     sync.Mutex // guards lastErr and lastFlush
	lastErr   string
	lastFlush time.Time
}

const maxStatus = 600

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
	if status < 0 || status >= maxStatus {
		status = 0
	}
	q.refused[status].Add(1)
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
			if buf = append(buf, it); len(buf) >= q.o.BatchSize {
				buf = q.flush(ctx, buf, false)
			}
		case <-tick.C:
			buf = q.flush(ctx, buf, false)
		case <-q.stop:
			q.drain(ctx, buf)
			return
		}
	}
}

// drain writes everything still queued; close() holds the write lock before stop, so nothing more
// can be enqueued.
func (q *queue) drain(ctx context.Context, buf []item) {
	for {
		select {
		case it := <-q.ch:
			if buf = append(buf, it); len(buf) >= q.o.BatchSize {
				buf = q.flush(ctx, buf, true)
			}
		default:
			q.flush(ctx, buf, true)
			return
		}
	}
}

// flush writes the calls of buf in one transaction, then applies its session closes, and returns buf
// emptied (the flush owns the reset; an empty buf is a no-op). A failed batch is retried with backoff:
// for the retry window with doubling backoff while the database is unreachable, otherwise Retries times
// (not when shutting down). One that keeps failing for a reason other than an unreachable database is
// retried record by record, each with the same small retry, so a single poison record cannot sink the rest.
func (q *queue) flush(parent context.Context, buf []item, final bool) []item {
	if len(buf) == 0 {
		return buf
	}
	var calls []contract.CallRecord
	var closes []contract.SessionClose
	for _, it := range buf {
		if it.call != nil {
			calls = append(calls, *it.call)
		} else {
			closes = append(closes, contract.SessionClose{WorkspaceID: it.closeWS, SessionID: it.closeID, At: it.closeAt})
		}
	}
	plan := retryPlan{final: final, deadline: time.Now().Add(q.o.RetryWindow)}
	ctx, cancel := context.WithTimeout(parent, q.o.RetryWindow+30*time.Second)
	defer cancel()
	if len(calls) > 0 {
		q.writeCalls(ctx, calls, plan)
	}
	if len(closes) > 0 {
		q.closeSessions(ctx, closes, plan)
	}
	return buf[:0]
}
