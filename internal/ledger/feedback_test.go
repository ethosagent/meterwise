package ledger_test

import (
	"context"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/ledger"
)

type fbStore struct {
	calls int
	err   error
}

func (f *fbStore) SetFeedback(_ context.Context, ws string, id int64, fb *string) (contract.MonitorEventRecord, error) {
	f.calls++
	return contract.MonitorEventRecord{ID: id, WorkspaceID: ws, Feedback: fb}, f.err
}

func TestFeedbackValidatesBeforeTheStore(t *testing.T) {
	correct, wrong, junk := contract.FeedbackCorrect, contract.FeedbackWrong, "maybe"
	tests := []struct {
		name  string
		fb    *string
		valid bool
	}{
		{"correct", &correct, true}, {"wrong", &wrong, true}, {"null clears", nil, true}, {"junk", &junk, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &fbStore{}
			ev, err := ledger.NewFeedback(st).SetFeedback(context.Background(), wsID, 7, tc.fb)
			if tc.valid {
				if err != nil || st.calls != 1 || ev.ID != 7 {
					t.Fatalf("ev = %+v err = %v calls = %d", ev, err, st.calls)
				}
				return
			}
			if contract.AsError(err).Code != contract.CodeValidationFailed || st.calls != 0 {
				t.Fatalf("err = %v calls = %d, want a 422 and no store call", err, st.calls)
			}
		})
	}
}
