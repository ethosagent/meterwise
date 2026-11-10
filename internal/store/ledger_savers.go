package store

import (
	"context"
	"encoding/json"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// saverCall is the trim, router and memory part of an InsertCalls element (plans 09-11). Empty strings are NULL and
// the money fields are micro-USD of the action's class.
type saverCall struct {
	TrimAction     string `json:"trimAction,omitempty"`
	TrimConfidence string `json:"trimConfidence,omitempty"`
	TrimTokens     int64  `json:"trimTokens,omitempty"`
	SavedTrim      int64  `json:"savedTrim,omitempty"`
	RouterAction   string `json:"routerAction,omitempty"`
	RoutedFrom     string `json:"routedFrom,omitempty"`
	RoutedTo       string `json:"routedTo,omitempty"`
	RouterScoreBp  *int32 `json:"routerScoreBp,omitempty"`
	SavedRouter    int64  `json:"savedRouter,omitempty"`
	HintInjected   bool   `json:"hintInjected,omitempty"`
}

func saverCallOf(r contract.CallRecord) saverCall {
	s := saverCall{TrimTokens: r.TrimTokens, SavedTrim: int64(r.SavedUSDTrim), RoutedFrom: r.RoutedFrom, RoutedTo: r.RoutedTo,
		SavedRouter: int64(r.SavedUSDRouter), HintInjected: r.HintInjected}
	if r.TrimAction != nil {
		s.TrimAction = string(*r.TrimAction)
	}
	if r.TrimConfidence != nil {
		s.TrimConfidence = string(*r.TrimConfidence)
	}
	if r.RouterAction != nil {
		s.RouterAction = string(*r.RouterAction)
	}
	if r.RouterScoreBp != nil {
		s.RouterScoreBp = new(clampInt32(int64(*r.RouterScoreBp)))
	}
	return s
}

// saverSession is the saver part of an UpsertSessions element: the totals of the batch's inserted calls.
type saverSession struct {
	TrimTokens  int64 `json:"trimTokens,omitempty"`
	RoutedCalls int32 `json:"routedCalls,omitempty"`
	Hints       int32 `json:"hints,omitempty"`
}

// addSavers folds one inserted call into its session aggregate: acted trim tokens, acted trim money (certain or
// likely by its confidence) and routed money into the two savings numbers, routed calls and injected hints. Observe
// is would-save and goes in neither number (non-negotiable 5).
func (a *sessionAgg) addSavers(r contract.CallRecord) {
	if r.TrimAction != nil && *r.TrimAction == contract.TrimActed {
		a.trimTokens += r.TrimTokens
		if r.TrimConfidence != nil && *r.TrimConfidence == contract.ConfidenceCertain {
			a.savedMicro += int64(r.SavedUSDTrim)
		} else {
			a.likelyMicro += int64(r.SavedUSDTrim)
		}
	}
	if r.RouterAction != nil && *r.RouterAction == contract.RouteRouted {
		a.routedCalls++
		a.savedMicro += int64(r.SavedUSDRouter)
	}
	if r.HintInjected {
		a.hints++
	}
}

// trimEventJSON is one element of the InsertTrimEvents array.
type trimEventJSON struct {
	OrgID       string `json:"orgId"`
	WorkspaceID string `json:"workspaceId"`
	SessionID   string `json:"sessionId"`
	CallID      string `json:"callId"`
	StartedUs   int64  `json:"startedUs"`
	Stage       string `json:"stage"`
	Tool        string `json:"tool,omitempty"`
	Regions     int32  `json:"regions"`
	BytesBefore int32  `json:"bytesBefore"`
	BytesAfter  int32  `json:"bytesAfter"`
	EstTokens   int64  `json:"estTokens"`
	Reverted    bool   `json:"reverted"`
	Action      string `json:"action"`
}

// writeTrimEvents inserts the trim stages of the calls a batch really inserted, in the batch's transaction.
// Replayed calls are not in fresh, so a repeat adds nothing; the unique key (workspace_id, call_id, stage) is the
// backstop. internal/ledger is the only caller (H4).
func writeTrimEvents(ctx context.Context, q *sqlc.Queries, fresh []contract.CallRecord) error {
	var events []trimEventJSON
	for _, r := range fresh {
		for _, e := range r.TrimEvents {
			events = append(events, trimEventJSON{
				OrgID: r.OrgID, WorkspaceID: r.WorkspaceID, SessionID: r.SessionID, CallID: r.ID, StartedUs: r.StartedAt.UnixMicro(),
				Stage: string(e.Stage), Tool: e.Tool, Regions: clampInt32(int64(e.Regions)), BytesBefore: clampInt32(int64(e.BytesBefore)),
				BytesAfter: clampInt32(int64(e.BytesAfter)), EstTokens: max(e.EstTokens, 0), Reverted: e.Reverted, Action: string(e.Action),
			})
		}
	}
	if len(events) == 0 {
		return nil
	}
	body, err := json.Marshal(events)
	if err != nil {
		return fail(ClassPool, "store: trim events are not JSON")
	}
	return q.InsertTrimEvents(ctx, body)
}
