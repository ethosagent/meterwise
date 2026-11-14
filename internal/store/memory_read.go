package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// MemoryStats reads what memory did over a validated range, on one snapshot so the counts agree: the traces stored
// now, the hits of the range by ts (injected, observe-only, hinted sessions, feedback), the likely money credited, and
// memory's own spend this UTC month. Mode and BudgetPaused are the caller's to fill. The likely number appears in no
// certain total (ADR 0027).
func (s *Store) MemoryStats(ctx context.Context, workspaceID string, r contract.SavingsRange, now time.Time) (contract.MemoryStats, error) {
	if !validID(workspaceID) {
		return contract.MemoryStats{}, contract.Invalid("workspaceId", "must be a workspace id")
	}
	from, to := window(r)
	month := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	var (
		traces int64
		hits   sqlc.MemoryHitStatsRow
		self   int64
	)
	err := s.readSnapshot(ctx, func(q *sqlc.Queries) (err error) {
		if traces, err = q.CountMemoryTraces(ctx, workspaceID); err != nil {
			return err
		}
		if hits, err = q.MemoryHitStats(ctx, sqlc.MemoryHitStatsParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)}); err != nil {
			return err
		}
		self, err = q.MemorySelfUsageSince(ctx, sqlc.MemorySelfUsageSinceParams{WorkspaceID: workspaceID, SinceTs: pgts(month)})
		return err
	})
	if err != nil {
		return contract.MemoryStats{}, err
	}
	return contract.MemoryStats{
		Range: r.Range, From: r.FromDay(), To: r.ToDay(), Traces: traces, HintedSessions: hits.HintedSessions, Hits: hits.Hits,
		ObservedHits: hits.ObservedHits, Helpful: hits.Helpful, Irrelevant: hits.Irrelevant,
		LikelySavedUsd: contract.MicroUSD(hits.LikelyMicro), SelfUsageUsdMonth: contract.MicroUSD(self),
	}, nil
}

// HitRow is a stored hit with what its hint is re-rendered from: the trace's approach, fix and files (all empty when
// the trace was forgotten).
type HitRow struct {
	ID              int64
	CallID          string
	TraceID         int64
	SimilarityBp    int
	Mode            contract.MemoryMode
	Injected        bool
	Feedback        *string
	EstSaved        contract.MicroUSD
	SourceSessionID string
	Approach        string
	Fix             string
	Files           []string
}

// maxSessionHits caps the hits of one session detail.
const maxSessionHits = 500

func hitRow(id int64, callID string, traceID int64, sim int32, mode string, injected bool, fb pgtype.Text, est int64, src, approach, fix string, sig []byte) HitRow {
	h := HitRow{
		ID: id, CallID: callID, TraceID: traceID, SimilarityBp: int(sim), Mode: contract.MemoryMode(mode), Injected: injected,
		EstSaved: contract.MicroUSD(est), SourceSessionID: src, Approach: approach, Fix: fix, Files: signatureOf(sig).Files,
	}
	if fb.Valid {
		h.Feedback = &fb.String
	}
	return h
}

// SessionMemoryHits lists the memory hits of one session of the workspace, in call order.
func (s *Store) SessionMemoryHits(ctx context.Context, workspaceID, sessionID string) ([]HitRow, error) {
	if !validID(workspaceID) {
		return nil, contract.Invalid("workspaceId", "must be a workspace id")
	}
	rows, err := s.q.ListSessionMemoryHits(ctx, sqlc.ListSessionMemoryHitsParams{WorkspaceID: workspaceID, SessionID: sessionID, MaxRows: maxSessionHits})
	if err != nil {
		return nil, queryErr(err)
	}
	out := make([]HitRow, len(rows))
	for i, r := range rows {
		out[i] = hitRow(r.ID, r.CallID, r.TraceID, r.SimilarityBp, r.Mode, r.Injected, r.Feedback, r.EstSavedMicro, r.SourceSessionID, r.Approach, r.Fix, r.Signature)
	}
	return out, nil
}

// MemoryHit reads one hit of the workspace; a hit of another workspace is not_found.
func (s *Store) MemoryHit(ctx context.Context, workspaceID string, hitID int64) (HitRow, error) {
	if !validID(workspaceID) {
		return HitRow{}, errNotFound
	}
	r, err := s.q.GetMemoryHit(ctx, sqlc.GetMemoryHitParams{WorkspaceID: workspaceID, ID: hitID})
	if err != nil {
		return HitRow{}, queryErr(err)
	}
	return hitRow(r.ID, r.CallID, r.TraceID, r.SimilarityBp, r.Mode, r.Injected, r.Feedback, r.EstSavedMicro, r.SourceSessionID, r.Approach, r.Fix, r.Signature), nil
}

// UncreditedHits lists the ids of a session's injected hits that have no likely credit yet.
func (s *Store) UncreditedHits(ctx context.Context, workspaceID, sessionID string) ([]int64, error) {
	if !validID(workspaceID) {
		return nil, nil
	}
	rows, err := s.q.ListSessionInjectedHits(ctx, sqlc.ListSessionInjectedHitsParams{WorkspaceID: workspaceID, SessionID: sessionID})
	if err != nil {
		return nil, queryErr(err)
	}
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out, nil
}

// Baseline is what a completed, un-hinted session of the workspace looked like over a window.
type Baseline struct {
	Sessions    int64
	MedianCalls float64
	Calls       int64
	Tokens      int64
	Cost        contract.MicroUSD
}

// SessionBaseline reads the baseline of the workspace's completed sessions with no hint since since, leaving the given
// session out.
func (s *Store) SessionBaseline(ctx context.Context, workspaceID, sessionID string, since time.Time) (Baseline, error) {
	if !validID(workspaceID) {
		return Baseline{}, nil
	}
	r, err := s.q.MemoryBaseline(ctx, sqlc.MemoryBaselineParams{WorkspaceID: workspaceID, SessionID: sessionID, SinceTs: pgts(since)})
	if err != nil {
		return Baseline{}, queryErr(err)
	}
	return Baseline{Sessions: r.Sessions, MedianCalls: r.MedianCalls, Calls: r.Calls, Tokens: r.Tokens, Cost: contract.MicroUSD(r.CostMicro)}, nil
}
