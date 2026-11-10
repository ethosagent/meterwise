package ledger_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/ethosagent/tollgate"
)

// A call refused before it reached the upstream (admission 429, provider policy or CIDR 403, an
// unknown or retired id 404/410) is not a call: no calls row, no session, so last_call_at and the
// stats do not move. The refusals are counted by status on /statusz instead.
func TestRefusedBeforeTheUpstreamIsCountedNotRecorded(t *testing.T) {
	w := &fakeWriter{}
	h := newHook(t, w, nil)
	for i, status := range []int{429, 403, 403, 404, 410} {
		c := directCall("refused-"+string(rune('a'+i)), "hi", nil, 1)
		if _, err := h.Before(context.Background(), c); err != nil {
			t.Fatal(err)
		}
		m := metricFor(c)
		m.Status, m.Finish = status, tollgate.FinishRejected
		h.After(context.Background(), c, m)
	}
	ok := directCall("ok-1", "hi", nil, 2)
	complete(t, h, ok)
	// an upstream that could not be reached did get a try: that one is a call (502, rejected)
	bad := directCall("bad-1", "hi", nil, 3)
	if _, err := h.Before(context.Background(), bad); err != nil {
		t.Fatal(err)
	}
	m := metricFor(bad)
	m.Status, m.Finish = 502, tollgate.FinishRejected
	h.After(context.Background(), bad, m)
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := w.snapshot()
	if len(got) != 2 || got[0].ID != "ok-1" || got[1].ID != "bad-1" {
		t.Fatalf("rows = %+v, want only the completed call and the unreachable-upstream call", got)
	}
	st := h.Status()
	want := map[string]int64{"403": 2, "404": 1, "410": 1, "429": 1}
	if !reflect.DeepEqual(st["refused_by_status"], want) {
		t.Fatalf("refused_by_status = %#v, want %v", st["refused_by_status"], want)
	}
	if st["enqueued"] != int64(2) || st["flushed"] != int64(2) || st["unpriced_calls"] != int64(0) {
		t.Fatalf("status = %v", st)
	}
}
