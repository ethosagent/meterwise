package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// eventJSON is one element of the InsertMonitorEvents array.
type eventJSON struct {
	OrgID       string                   `json:"orgId"`
	WorkspaceID string                   `json:"workspaceId"`
	SessionID   string                   `json:"sessionId"`
	CallID      string                   `json:"callId"`
	CallIndex   int32                    `json:"callIndex"`
	StartedUs   int64                    `json:"startedUs"`
	Monitor     contract.MonitorName     `json:"monitor"`
	Mode        contract.MonitorMode     `json:"mode"`
	Action      contract.MonitorMode     `json:"action"`
	Evidence    contract.MonitorEvidence `json:"evidence"`
	EstTokens   int64                    `json:"estTokens"`
	EstMicro    int64                    `json:"estMicro"`
	Confidence  contract.Confidence      `json:"confidence"`
}

// writeMonitorEvents inserts the verdicts of the calls a batch really inserted, in the batch's transaction.
// Replayed calls are not in fresh, so their events are never written twice; the unique key
// (workspace_id, call_id, monitor) is the backstop for a monitor that somehow reports twice.
func writeMonitorEvents(ctx context.Context, q *sqlc.Queries, fresh []contract.CallRecord) ([]contract.CallRecord, error) {
	var events []eventJSON
	for _, r := range fresh {
		for _, v := range r.Verdicts {
			events = append(events, eventJSON{
				OrgID: r.OrgID, WorkspaceID: r.WorkspaceID, SessionID: r.SessionID, CallID: r.ID, CallIndex: clampInt32(int64(r.CallIndex)),
				StartedUs: r.StartedAt.UnixMicro(), Monitor: v.Monitor, Mode: v.Mode, Action: v.Action, Evidence: v.Evidence,
				EstTokens: max(v.EstTokens, 0), EstMicro: int64(v.EstUSD), Confidence: v.Confidence,
			})
		}
	}
	if len(events) == 0 {
		return fresh, nil
	}
	body, err := json.Marshal(events)
	if err != nil {
		return nil, fail(ClassPool, "store: monitor events are not JSON")
	}
	rows, err := q.InsertMonitorEvents(ctx, body)
	if err != nil {
		return nil, err
	}
	return countedVerdicts(fresh, rows), nil
}

// countedVerdicts returns fresh with only the verdicts whose monitor_events row was inserted (named by the
// RETURNING rows), so the session sums can never include a verdict the table does not hold. The records
// are copied; fresh is not modified.
func countedVerdicts(fresh []contract.CallRecord, inserted []sqlc.InsertMonitorEventsRow) []contract.CallRecord {
	got := make(map[[2]string]bool, len(inserted))
	for _, r := range inserted {
		got[[2]string{r.CallID, r.Monitor}] = true
	}
	out := make([]contract.CallRecord, len(fresh))
	for i, r := range fresh {
		out[i] = r
		out[i].Verdicts = nil
		for _, v := range r.Verdicts {
			if k := [2]string{r.ID, string(v.Monitor)}; got[k] {
				got[k] = false // a repeat of the same (call, monitor) in one record counts once
				out[i].Verdicts = append(out[i].Verdicts, v)
			}
		}
	}
	return out
}

// SetFeedback sets (fb "correct" or "wrong") or clears (nil) the feedback of one monitor event of a
// workspace and returns the event. An unknown event, one of another workspace and a malformed workspace id
// are all not_found. internal/ledger owns the monitor_events writes (H4); the API reaches this only through
// the ledger's MonitorFeedback.
func (s *Store) SetFeedback(ctx context.Context, workspaceID string, eventID int64, fb *string) (contract.MonitorEventRecord, error) {
	if !validID(workspaceID) {
		return contract.MonitorEventRecord{}, errNotFound
	}
	p := sqlc.SetMonitorEventFeedbackParams{WorkspaceID: workspaceID, ID: eventID}
	if fb != nil {
		p.Feedback = pgtype.Text{String: *fb, Valid: true}
	}
	r, err := s.q.SetMonitorEventFeedback(ctx, p)
	if err != nil {
		return contract.MonitorEventRecord{}, queryErr(err)
	}
	return s.eventRecord(sqlc.ListSessionMonitorEventsRow(r)), nil
}

func (s *Store) eventRecord(r sqlc.ListSessionMonitorEventsRow) contract.MonitorEventRecord {
	ev := s.evidenceOf(r.ID, r.Evidence)
	var fb *string
	if r.Feedback.Valid {
		fb = &r.Feedback.String
	}
	return contract.MonitorEventRecord{
		ID: r.ID, OrgID: r.OrgID, WorkspaceID: r.WorkspaceID, SessionID: r.SessionID, CallID: r.CallID, CallIndex: int(r.CallIndex),
		TS: ts(r.Ts), Monitor: contract.MonitorName(r.Monitor), Mode: contract.MonitorMode(r.Mode), Action: contract.MonitorMode(r.Action),
		Evidence: ev, EstSavedToken: r.EstSavedTokens, EstSavedUSD: contract.MicroUSD(r.EstSavedMicro),
		Confidence: contract.Confidence(r.Confidence), Feedback: fb, CreatedAt: ts(r.CreatedAt),
	}
}

// SessionMonitorEvents returns a session's monitor events in call order (at most contract.MonitorEventCap).
func (s *Store) SessionMonitorEvents(ctx context.Context, workspaceID, sessionID string) ([]contract.MonitorEventRecord, error) {
	return s.eventsWith(ctx, s.q, workspaceID, sessionID)
}

func (s *Store) eventsWith(ctx context.Context, q *sqlc.Queries, workspaceID, sessionID string) ([]contract.MonitorEventRecord, error) {
	if !validID(workspaceID) {
		return nil, contract.Invalid("workspaceId", "must be a workspace id")
	}
	rows, err := q.ListSessionMonitorEvents(ctx, sqlc.ListSessionMonitorEventsParams{
		WorkspaceID: workspaceID, SessionID: sessionID, MaxRows: contract.MonitorEventCap,
	})
	if err != nil {
		return nil, queryErr(err)
	}
	out := make([]contract.MonitorEventRecord, len(rows))
	for i, r := range rows {
		out[i] = s.eventRecord(r)
	}
	return out, nil
}

// MonitorStats returns the six monitors' counts over [from, to) in MonitorNames order, zero-filled.
func (s *Store) MonitorStats(ctx context.Context, workspaceID string, from, to time.Time) ([]contract.MonitorStatRow, error) {
	if !validID(workspaceID) {
		return nil, contract.Invalid("workspaceId", "must be a workspace id")
	}
	rows, err := s.q.MonitorStats(ctx, sqlc.MonitorStatsParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)})
	if err != nil {
		return nil, queryErr(err)
	}
	by := make(map[contract.MonitorName]contract.MonitorStatRow, len(rows))
	for _, r := range rows {
		m := contract.MonitorName(r.Monitor)
		by[m] = contract.MonitorStatRow{
			Monitor: m, Fires: r.Fires, Blocked: r.Blocked, Noted: r.Noted, Observed: r.Observed,
			SavedTokens: r.SavedTokens, LikelySavedTokens: r.LikelySavedTokens, WouldSaveTokens: r.WouldSaveTokens,
			Saved: contract.MicroUSD(r.SavedMicro), Likely: contract.MicroUSD(r.LikelyMicro), WouldSave: contract.MicroUSD(r.WouldSaveMicro),
			FeedbackCorrect: r.FeedbackCorrect, FeedbackWrong: r.FeedbackWrong,
		}
	}
	names := contract.MonitorNames()
	out := make([]contract.MonitorStatRow, len(names))
	for i, n := range names {
		row := by[n]
		row.Monitor = n
		out[i] = row
	}
	return out, nil
}
