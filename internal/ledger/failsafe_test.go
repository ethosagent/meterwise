package ledger_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/ledger"
)

func closedList(w *fakeWriter) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.closed...)
}

func TestSessionCloseIsRetriedLikeACall(t *testing.T) {
	var attempts int
	w := &fakeWriter{failClose: func() error {
		attempts++
		if attempts <= 2 {
			return errors.New("store: database unreachable")
		}
		return nil
	}}
	h := newHook(t, w, func(o *ledger.Options) { o.RetryBackoff = time.Millisecond; o.FlushInterval = time.Millisecond })
	h.OnSessionClosed(context.Background(), tollgate.Session{ID: "s1", TenantID: wsID})
	deadline := time.Now().Add(5 * time.Second)
	for len(closedList(w)) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := h.Status()
	if len(closedList(w)) != 1 || st["lost_closes"] != int64(0) || st["sessions_closed"] != int64(1) {
		t.Fatalf("closed %v status %v", closedList(w), st)
	}
}

func TestSessionCloseThatKeepsFailingIsCountedAsLost(t *testing.T) {
	w := &fakeWriter{failClose: func() error { return errors.New("store: refused") }}
	h := newHook(t, w, func(o *ledger.Options) { o.RetryBackoff = time.Millisecond; o.FlushInterval = time.Hour })
	h.OnSessionClosed(context.Background(), tollgate.Session{ID: "s1", TenantID: wsID})
	h.OnSessionClosed(context.Background(), tollgate.Session{ID: "s2", TenantID: wsID})
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := h.Status()
	if st["lost_closes"] != int64(2) || st["sessions_closed"] != int64(0) || st["flush_errors"].(int64) < 1 {
		t.Fatalf("status %v", st)
	}
}

// The zero Options must keep privacy ON: a caller that forgets the field never stores message text.
func TestUnsetPrivacyModeFailsSafeToPrivacyOn(t *testing.T) {
	w := &fakeWriter{}
	h := newHook(t, w, nil) // PrivacyMode left nil
	complete(t, h, directCall("fs1", "some user text", &tollgate.Policy{Capture: true}, 1))
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := w.snapshot()[0].Preview; got != "" {
		t.Fatalf("preview stored with PrivacyMode unset: %q", got)
	}
}

func TestExplicitPrivacyOffStillAllowsPreview(t *testing.T) {
	w := &fakeWriter{}
	h := newHook(t, w, func(o *ledger.Options) { o.PrivacyMode = no() })
	complete(t, h, directCall("fs2", "some user text", &tollgate.Policy{Capture: true}, 1))
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := w.snapshot()[0].Preview; got == "" {
		t.Fatal("explicit PrivacyMode=false with capture on should keep the preview")
	}
}

func TestRecordFromAnotherOrgIsRefusedAndCounted(t *testing.T) {
	w := &fakeWriter{}
	h := newHook(t, w, nil)
	c := directCall("org-1", "hi", nil, 1)
	c.Tenant.Attrs["org_id"] = "99999999-9999-4999-8999-999999999999" // a valid uuid that is not the org
	complete(t, h, c)
	complete(t, h, directCall("org-2", "hi", nil, 1))
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := w.snapshot()
	st := h.Status()
	if len(got) != 1 || got[0].ID != "org-2" || st["skipped_wrong_org"] != int64(1) || st["enqueued"] != int64(1) {
		t.Fatalf("rows %+v status %v", got, st)
	}
}
