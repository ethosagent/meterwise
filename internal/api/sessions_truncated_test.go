package api_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

// A session with more calls than the detail cap (5000) says so instead of silently stopping.
func TestSessionDetailFlagsTruncationPastTheCap(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	now := time.Now().UTC().Add(-time.Hour)
	recs := make([]contract.CallRecord, 0, 5001)
	for i := 1; i <= 5001; i++ {
		recs = append(recs, callRec(ws, fmt.Sprintf("big-%05d", i), "big", i, now.Add(time.Duration(i)*time.Millisecond), nil))
	}
	recs = append(recs, callRec(ws, "small-1", "small", 1, now, nil))
	s.seedCalls(t, recs...)

	big := decodeAs[contract.SessionDetail](t, s.must(t, admin.tok, "GET", "/api/sessions/big?workspaceId="+ws, "", 200))
	if len(big.Calls) != 5000 || big.Session.Calls != 5001 || !big.Truncated {
		t.Fatalf("big: %d calls of %d, truncated=%v", len(big.Calls), big.Session.Calls, big.Truncated)
	}
	small := decodeAs[contract.SessionDetail](t, s.must(t, admin.tok, "GET", "/api/sessions/small?workspaceId="+ws, "", 200))
	if len(small.Calls) != 1 || small.Truncated {
		t.Fatalf("small: %d calls, truncated=%v", len(small.Calls), small.Truncated)
	}
}
