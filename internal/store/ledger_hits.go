package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// MemoryHit is one recall decision of a call, as the ledger carries it from Meta[memory] to the batch that writes the
// call (plan 09). It holds ids and numbers only: the hint text is never stored, it is re-rendered from the trace.
type MemoryHit struct {
	CallID       string
	TraceID      int64
	SimilarityBp int
	Mode         contract.MemoryMode
	Injected     bool
}

type hitJSON struct {
	OrgID        string `json:"orgId"`
	WorkspaceID  string `json:"workspaceId"`
	SessionID    string `json:"sessionId"`
	CallID       string `json:"callId"`
	TraceID      int64  `json:"traceId"`
	SimilarityBp int32  `json:"similarityBp"`
	Mode         string `json:"mode"`
	Injected     bool   `json:"injected"`
	StartedUs    int64  `json:"startedUs"`
}

// writeMemoryHits inserts the hits of the calls a batch really inserted, in the batch's transaction. A replayed call
// is not in fresh, so it adds no hit; the unique key (workspace_id, call_id) is the backstop. internal/ledger is the
// only caller (H4).
func writeMemoryHits(ctx context.Context, q *sqlc.Queries, fresh []contract.CallRecord, hits []MemoryHit) error {
	if len(hits) == 0 {
		return nil
	}
	byCall := make(map[string]MemoryHit, len(hits)) // hits are few, records many: index the hits
	for _, h := range hits {
		byCall[h.CallID] = h
	}
	out := make([]hitJSON, 0, len(hits))
	for _, r := range fresh {
		h, ok := byCall[r.ID]
		if !ok || h.TraceID <= 0 || (h.Mode != contract.MemoryModeObserve && h.Mode != contract.MemoryModeHint) {
			continue
		}
		out = append(out, hitJSON{
			OrgID: r.OrgID, WorkspaceID: r.WorkspaceID, SessionID: r.SessionID, CallID: r.ID, TraceID: h.TraceID,
			SimilarityBp: clampInt32(min(int64(h.SimilarityBp), 10000)), Mode: string(h.Mode), Injected: h.Injected, StartedUs: r.StartedAt.UnixMicro(),
		})
	}
	if len(out) == 0 {
		return nil
	}
	body, err := json.Marshal(out)
	if err != nil {
		return fail(ClassPool, "store: memory hits are not JSON")
	}
	_, err = q.InsertMemoryHits(ctx, body)
	return err
}

// HitRef names the trace a hit belongs to.
type HitRef struct {
	HitID   int64
	TraceID int64
}

// SetMemoryHitFeedback sets ("helpful" or "irrelevant") or clears (nil) the feedback of one hit of a workspace. A hit
// that is not the workspace's, or a malformed workspace id, is not_found. internal/ledger owns the memory_hits writes
// (H4); the API reaches this only through the ledger's memory port.
func (s *Store) SetMemoryHitFeedback(ctx context.Context, workspaceID string, hitID int64, fb *string) (HitRef, error) {
	if !validID(workspaceID) {
		return HitRef{}, errNotFound
	}
	p := sqlc.SetMemoryHitFeedbackParams{WorkspaceID: workspaceID, ID: hitID}
	if fb != nil {
		p.Feedback = pgtype.Text{String: *fb, Valid: true}
	}
	r, err := s.q.SetMemoryHitFeedback(ctx, p)
	if err != nil {
		return HitRef{}, queryErr(err)
	}
	return HitRef{HitID: r.ID, TraceID: r.TraceID}, nil
}

// CreditMemoryHit sets the likely credit of one injected hit once and adds the money to its session's likely number,
// in one transaction; it reports whether it credited (a hit already credited, one that was only observed and one of
// another session or workspace credit nothing). Certain money is never touched.
func (s *Store) CreditMemoryHit(ctx context.Context, workspaceID, sessionID string, hitID, tokens int64, usd contract.MicroUSD) (bool, error) {
	if !validID(workspaceID) || usd < 0 || tokens < 0 || (usd == 0 && tokens == 0) {
		return false, nil
	}
	credited := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		_, err := q.CreditMemoryHit(ctx, sqlc.CreditMemoryHitParams{WorkspaceID: workspaceID, ID: hitID, SessionID: sessionID, Tokens: tokens, UsdMicro: int64(usd)})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		credited = true
		return q.AddSessionLikelySaved(ctx, sqlc.AddSessionLikelySavedParams{WorkspaceID: workspaceID, ID: sessionID, UsdMicro: int64(usd)})
	})
	return credited, queryErr(err)
}
