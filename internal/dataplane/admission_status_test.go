package dataplane

import (
	"context"
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
