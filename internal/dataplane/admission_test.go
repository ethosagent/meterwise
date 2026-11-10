package dataplane_test

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/dataplane"
)

const hold = 1500 * time.Millisecond // how long a fake upstream call keeps its slot

type result struct {
	code       int
	retryAfter string
}

// start fires a call in the background and waits until the upstream has it, so the slot is taken.
func (r *rig) start(t *testing.T, id string, wg *sync.WaitGroup) {
	t.Helper()
	want := len(r.up.Requests()) + 1
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, _ := r.do(t, "/w/"+id+wires[0].path, wires[0].fixture, wires[0].hdr)
		if resp.StatusCode != 200 {
			t.Errorf("held call: status %d", resp.StatusCode)
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for len(r.up.Requests()) < want {
		if time.Now().After(deadline) {
			t.Fatal("the call never reached the upstream")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (r *rig) try(t *testing.T, id string) result {
	t.Helper()
	resp, _ := r.do(t, "/w/"+id+wires[0].path, wires[0].fixture, wires[0].hdr)
	return result{resp.StatusCode, resp.Header.Get("Retry-After")}
}

func TestAdmissionCapsInFlightCallsPerWorkspaceThen429WithRetryAfter(t *testing.T) {
	r := newRig(t, rigOpts{up: testkit.Options{FirstByteDelay: hold}, max: 1, queue: 100 * time.Millisecond})
	var wg sync.WaitGroup
	r.start(t, curID, &wg)
	start := time.Now()
	got := r.try(t, curID)
	if got.code != http.StatusTooManyRequests {
		t.Fatalf("second call: %d, want 429", got.code)
	}
	if secs, err := strconv.Atoi(got.retryAfter); err != nil || secs < 1 {
		t.Fatalf("Retry-After = %q", got.retryAfter)
	}
	if time.Since(start) < 90*time.Millisecond {
		t.Fatal("the call did not wait out the queue timeout before it was refused")
	}
	if n := len(r.up.Requests()); n != 1 {
		t.Fatalf("the refused call reached the upstream (%d calls)", n)
	}
	// another workspace has its own slots
	r.dir.mu.Lock()
	r.dir.capSet = nil
	r.dir.mu.Unlock()
	if other := r.try(t, capID); other.code != 200 {
		t.Fatalf("a saturated workspace starved another: %d", other.code)
	}
	wg.Wait()
	if again := r.try(t, curID); again.code != 200 {
		t.Fatalf("after the slot freed: %d", again.code)
	}
}

func TestTheWorkspaceOverrideWinsAndChangesApplyWithoutARestart(t *testing.T) {
	r := newRig(t, rigOpts{up: testkit.Options{FirstByteDelay: hold}, max: 1, queue: 100 * time.Millisecond})
	set := func(v any) {
		r.dir.mu.Lock()
		defer r.dir.mu.Unlock()
		r.dir.capSet = map[string]any{"maxConcurrent": v}
	}
	var wg sync.WaitGroup
	set(float64(2)) // settings JSON numbers arrive as float64
	r.start(t, capID, &wg)
	r.start(t, capID, &wg)
	if got := r.try(t, capID); got.code != 429 {
		t.Fatalf("third call under an override of 2: %d, want 429", got.code)
	}
	set(float64(3)) // an admin raises it: the next call sees it, no restart
	r.start(t, capID, &wg)
	wg.Wait()
	set(nil) // override removed: the default of 1 applies again
	r.start(t, capID, &wg)
	if got := r.try(t, capID); got.code != 429 {
		t.Fatalf("after removing the override the default cap must apply: %d", got.code)
	}
	wg.Wait()
}

func TestNewAdmissionValidatesAndReportsStatus(t *testing.T) {
	if _, err := dataplane.NewAdmission(0, time.Second); err == nil {
		t.Fatal("a cap of 0 must be refused")
	}
	a, err := dataplane.NewAdmission(8, dataplane.QueueTimeout)
	if err != nil {
		t.Fatal(err)
	}
	st := a.Status()
	if st["default_max"] != 8 || st["queue_timeout_ms"] != int64(5000) {
		t.Fatalf("status %+v", st)
	}
	if _, err := a.Acquire(context.Background(), tollgateTenant("huge", "999999"), nil); err != nil {
		t.Fatalf("an override above the clamp must still admit: %v", err)
	}
}
