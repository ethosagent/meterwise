package dataplane

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"
)

func tenantWithCap(id, n string) tollgate.Tenant {
	return tollgate.Tenant{ID: id, Attrs: map[string]string{"max_concurrent": n}}
}

// TestStatusCountsInFlightRejectionsAndLimiterErrors: concurrency.Limiter (tollgate v0.1.0)
// exposes no counters, so Admission keeps its own.
func TestStatusCountsInFlightRejectionsAndLimiterErrors(t *testing.T) {
	a, err := NewAdmission(1, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	get := func(k string) int64 {
		t.Helper()
		v, ok := a.Status()[k].(int64)
		if !ok {
			t.Fatalf("status %q missing or not an int64: %+v", k, a.Status())
		}
		return v
	}
	rel, err := a.Acquire(ctx, tollgate.Tenant{ID: "a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if get("in_flight") != 1 {
		t.Fatalf("in_flight = %d, want 1", get("in_flight"))
	}
	if _, err := a.Acquire(ctx, tollgate.Tenant{ID: "a"}, nil); err == nil {
		t.Fatal("second call must be refused")
	}
	if get("rejected") != 1 || get("in_flight") != 1 {
		t.Fatalf("after a 429: %+v", a.Status())
	}
	rel()
	rel() // idempotent: must not drive the gauge negative
	if get("in_flight") != 0 {
		t.Fatalf("in_flight = %d after release, want 0", get("in_flight"))
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	rel2, _ := a.Acquire(ctx, tollgate.Tenant{ID: "b"}, nil)
	if _, err := a.Acquire(cctx, tollgate.Tenant{ID: "b"}, nil); err == nil {
		t.Fatal("a cancelled wait must fail")
	}
	rel2()
	if get("rejected") != 1 || get("abandoned") != 1 {
		t.Fatalf("a cancelled wait is abandoned, not a 429: %+v", a.Status())
	}
	// a limiter that cannot be built falls back to the default cap and is counted
	a.queue = -1
	rel3, err := a.Acquire(ctx, tenantWithCap("c", "7"), nil)
	if err != nil {
		t.Fatalf("fallback to the default limiter: %v", err)
	}
	rel3()
	if get("limiter_errors") != 1 {
		t.Fatalf("limiter_errors = %d, want 1", get("limiter_errors"))
	}
}

// A release may be called more than once (tollgate's limiter tolerates it); the in-flight gauge and
// the slot must each move exactly once, even when the calls race.
func TestConcurrentReleasesFreeOneSlotAndMoveTheGaugeOnce(t *testing.T) {
	a, err := NewAdmission(1, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ten := tollgate.Tenant{ID: "a"}
	rel, err := a.Acquire(ctx, ten, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); rel() }()
	}
	wg.Wait()
	if n := a.Status()["in_flight"]; n != int64(0) {
		t.Fatalf("in_flight = %v after racing releases, want 0", n)
	}
	r1, err := a.Acquire(ctx, ten, nil)
	if err != nil {
		t.Fatalf("the slot was not freed: %v", err)
	}
	if _, err := a.Acquire(ctx, ten, nil); err == nil {
		t.Fatal("a repeated release freed more than one slot")
	}
	r1()
}

func TestAcquireAndReleaseAllocationBudget(t *testing.T) {
	a, err := NewAdmission(4, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, ten := context.Background(), tollgate.Tenant{ID: "a"}
	n := testing.AllocsPerRun(200, func() {
		rel, _ := a.Acquire(ctx, ten, nil)
		rel()
	})
	t.Logf("allocs per Acquire+release: %v", n)
	if n > 6 {
		t.Fatalf("%v allocs per Acquire+release, budget 6", n)
	}
}
