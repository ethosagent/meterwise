package memory

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Job limits: at most two jobs run at once per workspace, each workspace queues at most wsQueue and the whole engine at
// most maxDepth; a job over a limit is dropped and counted, never waited for (nothing here may slow a request).
const (
	workersPerWS = 2
	wsQueueCap   = 64
	maxDepth     = 1024
	jobTimeout   = 30 * time.Second
	idleExit     = 30 * time.Second
	closeWait    = 5 * time.Second
)

type jobKind int

const (
	kindRecall jobKind = iota
	kindDistil
	kindFailure
	kindCredit
	kindTouch
	numKinds
)

var kindNames = [numKinds]string{"recall", "distil", "failure", "credit", "touch"}

type job struct {
	kind jobKind
	fn   func(ctx context.Context) error
}

type wsq struct {
	ch      chan job
	workers int
}

// jobs runs background work in per-workspace queues with their own small worker sets, started on demand and gone when
// idle.
type jobs struct {
	ctx    context.Context
	log    *slog.Logger
	wg     sync.WaitGroup
	mu     sync.Mutex
	qs     map[string]*wsq
	closed bool
	depth  atomic.Int64 // queued, not yet picked up
	active atomic.Int64 // queued or running: zero means every accepted job has finished

	done, failed, dropped [numKinds]atomic.Int64
}

func newJobs(ctx context.Context, log *slog.Logger) *jobs {
	return &jobs{ctx: ctx, log: log, qs: map[string]*wsq{}}
}

// enqueue queues fn for the workspace and reports whether it was accepted. The job outlives ctx (the response is
// already sent when After runs); the worker that runs it is started with ctx's values and none of its cancellation.
func (j *jobs) enqueue(ctx context.Context, ws string, k jobKind, fn func(ctx context.Context) error) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.depth.Load() >= maxDepth {
		j.dropped[k].Add(1)
		return false
	}
	q := j.qs[ws]
	if q == nil {
		q = &wsq{ch: make(chan job, wsQueueCap)}
		j.qs[ws] = q
	}
	j.active.Add(1)
	select {
	case q.ch <- job{kind: k, fn: fn}:
		j.depth.Add(1)
	default:
		j.active.Add(-1)
		j.dropped[k].Add(1)
		return false
	}
	if q.workers < workersPerWS {
		q.workers++
		j.wg.Add(1)
		go j.work(context.WithoutCancel(ctx), ws, q)
	}
	return true
}

func (j *jobs) work(ctx context.Context, ws string, q *wsq) {
	defer j.wg.Done()
	idle := time.NewTimer(idleExit)
	defer idle.Stop()
	for {
		select {
		case <-j.ctx.Done():
			return
		case jb := <-q.ch:
			j.depth.Add(-1)
			j.run(ctx, jb)
			j.active.Add(-1)
			idle.Reset(idleExit) // Go 1.23+ timers: Reset needs no drain
		case <-idle.C:
			j.mu.Lock()
			if len(q.ch) == 0 {
				q.workers--
				if q.workers == 0 {
					delete(j.qs, ws)
				}
				j.mu.Unlock()
				return
			}
			j.mu.Unlock()
			idle.Reset(idleExit)
		}
	}
}

// run executes one job under a timeout and a panic guard.
func (j *jobs) run(workerCtx context.Context, jb job) {
	ctx, cancel := context.WithTimeout(workerCtx, jobTimeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			j.failed[jb.kind].Add(1)
		}
	}()
	if err := jb.fn(ctx); err != nil {
		j.failed[jb.kind].Add(1)
		j.log.Debug("memory job failed", "kind", kindNames[jb.kind], "err", err.Error())
		return
	}
	j.done[jb.kind].Add(1)
}

// close refuses new jobs and waits briefly for the running ones (the engine's context is already cancelled).
func (j *jobs) close() {
	j.mu.Lock()
	j.closed = true
	j.mu.Unlock()
	done := make(chan struct{})
	go func() { j.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(closeWait):
	}
}

func (j *jobs) status() map[string]any {
	by := map[string]any{}
	for k := range numKinds {
		by[kindNames[k]] = map[string]any{"done": j.done[k].Load(), "failed": j.failed[k].Load(), "dropped": j.dropped[k].Load()}
	}
	j.mu.Lock()
	workspaces := len(j.qs)
	j.mu.Unlock()
	return map[string]any{"queue_depth": j.depth.Load(), "jobs_active": j.active.Load(), "workspaces": workspaces, "jobs": by}
}
