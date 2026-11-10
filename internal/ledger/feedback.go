package ledger

import (
	"context"

	"github.com/ethosagent/meterwise/internal/contract"
)

// FeedbackStore is the write the ledger needs for monitor feedback; *store.Store implements it.
type FeedbackStore interface {
	SetFeedback(ctx context.Context, workspaceID string, eventID int64, fb *string) (contract.MonitorEventRecord, error)
}

// Feedback implements contract.MonitorFeedback. monitor_events is a ledger table (H4: only the ledger writes
// it), so the API reaches the one UPDATE through this type, not through the store.
type Feedback struct{ st FeedbackStore }

var _ contract.MonitorFeedback = (*Feedback)(nil)

// NewFeedback wraps the store write.
func NewFeedback(st FeedbackStore) *Feedback { return &Feedback{st: st} }

// SetFeedback sets ("correct" or "wrong") or clears (nil) the feedback of one event of a workspace and
// returns the event. A value other than the two is a 422 on "feedback"; an event that is not in the workspace
// is not_found, whoever asks.
func (f *Feedback) SetFeedback(ctx context.Context, workspaceID string, eventID int64, fb *string) (contract.MonitorEventRecord, error) {
	if !contract.ValidFeedback(fb) {
		return contract.MonitorEventRecord{}, contract.Invalid("feedback", `must be "correct", "wrong" or null`)
	}
	return f.st.SetFeedback(ctx, workspaceID, eventID, fb)
}
