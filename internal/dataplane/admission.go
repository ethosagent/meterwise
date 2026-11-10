package dataplane

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethosagent/tollgate"
	"github.com/ethosagent/tollgate/ext/admission/concurrency"

	"github.com/ethosagent/meterwise/internal/wsauth"
)

// QueueTimeout is how long a call waits for a slot before it is answered 429.
const QueueTimeout = 5 * time.Second

// RetryAfter is the Retry-After hint on a 429 from admission.
const RetryAfter = 2 * time.Second

// Admission caps the in-flight calls of each workspace. The default cap is
// MAX_CONCURRENT_PER_WORKSPACE; a workspace with settings.maxConcurrent uses that instead, and a
// change of the setting applies to the next call with no restart.
//
// How: tollgate's concurrency.Limiter fixes a tenant's cap when its bucket is created and takes a
// per-tenant map only at construction. So the override rides on the Tenant (wsauth puts it in
// Attrs["max_concurrent"] when the directory cache is filled, and the directory busts the entry on
// every settings update) and Admission keeps one Limiter per distinct cap value, created on first
// use: the default plus at most wsauth.MaxConcurrentCap more. A call goes to the limiter for its
// workspace's current cap. While a call admitted under the old cap is still in flight, the workspace
// can briefly hold old-cap plus new-cap slots at once (the old call finishes on its old limiter);
// nothing is lost or leaked. See docs/decisions/0021.
type Admission struct {
	def   *concurrency.Limiter
	max   int
	queue time.Duration

	mu    sync.RWMutex
	byCap map[int]*concurrency.Limiter

	// concurrency.Limiter exposes no counters (tollgate v0.1.0), so Admission keeps its own.
	inFlight, rejected, abandoned, limiterErrs atomic.Int64
}

var _ tollgate.Admission = (*Admission)(nil)

// NewAdmission builds the admission with defaultMax slots per workspace (must be > 0). A call
// waits up to queue (QueueTimeout in production) for a slot.
func NewAdmission(defaultMax int, queue time.Duration) (*Admission, error) {
	l, err := newLimiter(defaultMax, queue)
	if err != nil {
		return nil, err
	}
	return &Admission{def: l, max: defaultMax, queue: queue, byCap: map[int]*concurrency.Limiter{defaultMax: l}}, nil
}

func newLimiter(n int, queue time.Duration) (*concurrency.Limiter, error) {
	return concurrency.New(concurrency.Config{DefaultMax: n, QueueTimeout: queue, RetryAfter: RetryAfter})
}

// Acquire implements tollgate.Admission; past QueueTimeout it returns the limiter's 429 with a
// Retry-After hint.
func (a *Admission) Acquire(ctx context.Context, t tollgate.Tenant, c *tollgate.Call) (func(), error) {
	l := a.def
	if s := t.Attrs[wsauth.AttrMaxConcurrent]; s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n != a.max {
			var lerr error
			if l, lerr = a.limiter(n); lerr != nil {
				a.limiterErrs.Add(1) // fail closed to the default cap, and count it
				l = a.def
			}
		}
	}
	release, err := l.Acquire(ctx, t, c)
	if err != nil {
		var se interface{ HTTPStatus() int }
		if errors.As(err, &se) && se.HTTPStatus() == http.StatusTooManyRequests {
			a.rejected.Add(1)
		} else {
			a.abandoned.Add(1) // the caller went away while queued
		}
		return nil, err
	}
	a.inFlight.Add(1)
	var once sync.Once
	return func() {
		once.Do(func() {
			a.inFlight.Add(-1)
			release()
		})
	}, nil
}

func (a *Admission) limiter(n int) (*concurrency.Limiter, error) {
	a.mu.RLock()
	l, ok := a.byCap[n]
	a.mu.RUnlock()
	if ok {
		return l, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if l, ok = a.byCap[n]; ok {
		return l, nil
	}
	l, err := newLimiter(n, a.queue)
	if err != nil {
		return nil, err
	}
	a.byCap[n] = l
	return l, nil
}

// Status implements contract.StatusSource.
func (a *Admission) Status() map[string]any {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return map[string]any{
		"default_max": a.max, "limiters": len(a.byCap), "queue_timeout_ms": a.queue.Milliseconds(),
		"in_flight": a.inFlight.Load(), "rejected": a.rejected.Load(), "abandoned": a.abandoned.Load(),
		"limiter_errors": a.limiterErrs.Load(),
	}
}

// Name labels the source on /statusz.
func (*Admission) Name() string { return "admission" }
