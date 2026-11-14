package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// EmbedDim is the dimension of memory_traces.embedding (vector(1536)).
const EmbedDim = 1536

// NewTrace is a distilled session ready to store. Embedding must have EmbedDim values.
type NewTrace struct {
	WorkspaceID  string
	SessionID    string
	TaskSummary  string
	Observations []string
	Approach     string
	Fix          string
	Signature    contract.TraceSignature
	Confidence   float32
	PromptV      int
	EmbedModel   string
	Embedding    []float32
}

// Candidate is one trace the recall query found: what the hint is rendered from, and how close it is.
type Candidate struct {
	ID         int64
	SessionID  string
	Approach   string
	Fix        string
	Signature  contract.TraceSignature
	Similarity float64 // cosine similarity, 1 - distance
}

// vectorLiteral renders v as a pgvector text literal ('[0.1,0.2]'). It refuses a vector of the wrong dimension or with
// a NaN or infinite value, which the column would reject anyway (ADR 0031: no Go vector dependency).
func vectorLiteral(v []float32) (string, error) {
	if len(v) != EmbedDim {
		return "", fmt.Errorf("store: an embedding has %d values, the column holds %d", len(v), EmbedDim)
	}
	var b strings.Builder
	b.Grow(len(v) * 10)
	b.WriteByte('[')
	for i, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return "", errors.New("store: an embedding value is not finite")
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String(), nil
}

// InsertTrace stores a trace. It reports false, with no error, when the session already has one (a replayed close).
// memory is the only writer of memory_traces.
func (s *Store) InsertTrace(ctx context.Context, t NewTrace) (id int64, inserted bool, err error) {
	if !validID(t.WorkspaceID) {
		return 0, false, contract.Invalid("workspaceId", "must be a workspace id")
	}
	lit, err := vectorLiteral(t.Embedding)
	if err != nil {
		return 0, false, err
	}
	obs, _ := json.Marshal(nonNilStrings(t.Observations))
	sig, _ := json.Marshal(normSignature(t.Signature))
	id, err = s.q.InsertMemoryTrace(ctx, sqlc.InsertMemoryTraceParams{
		OrgID: orgID, WorkspaceID: t.WorkspaceID, SessionID: t.SessionID, TaskSummary: t.TaskSummary, Observations: obs,
		Approach: t.Approach, Fix: t.Fix, Signature: sig, BaseConfidence: t.Confidence, DistilPromptV: clampInt32(int64(t.PromptV)),
		EmbedModel: t.EmbedModel, Embedding: lit,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, queryErr(err)
	}
	return id, true, nil
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func normSignature(s contract.TraceSignature) contract.TraceSignature {
	s.Files, s.Errors, s.Tools = nonNilStrings(s.Files), nonNilStrings(s.Errors), nonNilStrings(s.Tools)
	return s
}

func signatureOf(raw []byte) contract.TraceSignature {
	var sig contract.TraceSignature
	_ = json.Unmarshal(raw, &sig)
	return normSignature(sig)
}

// TouchTrace records that a hint was injected from the trace.
func (s *Store) TouchTrace(ctx context.Context, workspaceID string, traceID int64) error {
	if !validID(workspaceID) {
		return nil
	}
	return queryErr(s.q.TouchMemoryTrace(ctx, sqlc.TouchMemoryTraceParams{WorkspaceID: workspaceID, ID: traceID}))
}

// RecountTraceVotes recomputes a trace's votes from its hits' feedback; the stored confidence follows (three
// downvotes and no upvote demote it by 0.2, three upvotes promote it by 0.2). It returns the confidence in basis points.
func (s *Store) RecountTraceVotes(ctx context.Context, workspaceID string, traceID int64) (int, error) {
	if !validID(workspaceID) {
		return 0, errNotFound
	}
	bp, err := s.q.RecountMemoryTraceVotes(ctx, sqlc.RecountMemoryTraceVotesParams{WorkspaceID: workspaceID, ID: traceID})
	if errors.Is(err, pgx.ErrNoRows) { // the trace was forgotten: nothing to recount
		return 0, nil
	}
	return int(bp), queryErr(err)
}

func traceSummary(id int64, session, task string, sig []byte, uses, up, down int32, confBp int32, last, created pgtype.Timestamptz) contract.TraceSummary {
	s := contract.TraceSummary{
		ID: strconv.FormatInt(id, 10), SessionID: session, TaskSummary: task, Files: signatureOf(sig).Files, Uses: int64(uses),
		Upvotes: int64(up), Downvotes: int64(down), ConfidenceBp: int(confBp), CreatedAt: rfc3339(created),
	}
	if last.Valid {
		s.LastUsedAt = new(rfc3339(last))
	}
	return s
}

// ListTraces returns one page of the workspace's traces, newest first behind an opaque cursor.
func (s *Store) ListTraces(ctx context.Context, workspaceID, cursor string, limit int) (contract.TraceList, error) {
	limit, err := contract.ResolvePageLimit(limit)
	if err != nil {
		return contract.TraceList{}, err
	}
	if !validID(workspaceID) {
		return contract.TraceList{}, contract.Invalid("workspaceId", "must be a workspace id")
	}
	at, rawID, err := decodeCursor(cursor)
	if err != nil {
		return contract.TraceList{}, err
	}
	p := sqlc.ListMemoryTracesParams{WorkspaceID: workspaceID, MaxRows: clampInt32(int64(limit + 1))}
	if cursor != "" {
		id, perr := strconv.ParseInt(rawID, 10, 64)
		if perr != nil || id <= 0 {
			return contract.TraceList{}, contract.Invalid("cursor", "is not a cursor this API issued")
		}
		p.CursorAt, p.CursorID = pgts(at), pgtype.Int8{Int64: id, Valid: true}
	}
	rows, err := s.q.ListMemoryTraces(ctx, p)
	if err != nil {
		return contract.TraceList{}, queryErr(err)
	}
	out := contract.TraceList{Items: make([]contract.TraceSummary, 0, min(len(rows), limit))}
	for i, r := range rows {
		if i == limit { // the extra row only proves there is a next page
			last := rows[limit-1]
			out.NextCursor = new(encodeCursor(ts(last.CreatedAt), strconv.FormatInt(last.ID, 10)))
			break
		}
		out.Items = append(out.Items, traceSummary(r.ID, r.SessionID, r.TaskSummary, r.Signature, r.Uses, r.Upvotes, r.Downvotes, r.ConfidenceBp, r.LastUsedAt, r.CreatedAt))
	}
	return out, nil
}

// maxHelpedSessions is the cap of TraceDetail.HelpedSessions.
const maxHelpedSessions = 50

// TraceDetail reads one trace of the workspace; a trace of another workspace, an unknown or a malformed id is not_found.
func (s *Store) TraceDetail(ctx context.Context, workspaceID, traceID string) (contract.TraceDetail, error) {
	id, err := strconv.ParseInt(traceID, 10, 64)
	if err != nil || id <= 0 || !validID(workspaceID) {
		return contract.TraceDetail{}, errNotFound
	}
	var (
		r       sqlc.GetMemoryTraceRow
		helped  []string
		readErr error
	)
	readErr = s.readSnapshot(ctx, func(q *sqlc.Queries) (err error) {
		if r, err = q.GetMemoryTrace(ctx, sqlc.GetMemoryTraceParams{WorkspaceID: workspaceID, ID: id}); err != nil {
			return err
		}
		helped, err = q.ListTraceHelpedSessions(ctx, sqlc.ListTraceHelpedSessionsParams{WorkspaceID: workspaceID, TraceID: id, MaxRows: maxHelpedSessions})
		return err
	})
	if readErr != nil {
		return contract.TraceDetail{}, readErr
	}
	var obs []string
	_ = json.Unmarshal(r.Observations, &obs)
	d := contract.TraceDetail{
		TraceSummary: traceSummary(r.ID, r.SessionID, r.TaskSummary, r.Signature, r.Uses, r.Upvotes, r.Downvotes, r.ConfidenceBp, r.LastUsedAt, r.CreatedAt),
		Observations: nonNilStrings(obs), Approach: r.Approach, Fix: r.Fix, Signature: signatureOf(r.Signature), HelpedSessions: nonNilStrings(helped),
	}
	return d, nil
}

// ForgetTraces deletes every trace and failure record of the workspace and says how many traces went.
func (s *Store) ForgetTraces(ctx context.Context, workspaceID string) (int64, error) {
	if !validID(workspaceID) {
		return 0, contract.Invalid("workspaceId", "must be a workspace id")
	}
	n, err := s.q.DeleteWorkspaceMemoryTraces(ctx, workspaceID)
	return n, queryErr(err)
}

// PruneMemory deletes the org's traces unused since cutoff and its failure records older than it.
func (s *Store) PruneMemory(ctx context.Context, cutoff time.Time) (int64, error) {
	n, err := s.q.PruneMemory(ctx, sqlc.PruneMemoryParams{OrgID: orgID, Cutoff: pgts(cutoff)})
	return n, queryErr(err)
}

// TraceFailure is the light record of a session that did not end in a solution.
type TraceFailure struct {
	WorkspaceID string
	SessionID   string
	Signature   contract.TraceSignature
	LastError   string
	Calls       int
}

// InsertTraceFailure stores a failure record; one per session, a repeat adds nothing.
func (s *Store) InsertTraceFailure(ctx context.Context, f TraceFailure) error {
	if !validID(f.WorkspaceID) {
		return contract.Invalid("workspaceId", "must be a workspace id")
	}
	sig, _ := json.Marshal(normSignature(f.Signature))
	return queryErr(s.q.InsertMemoryTraceFailure(ctx, sqlc.InsertMemoryTraceFailureParams{
		OrgID: orgID, WorkspaceID: f.WorkspaceID, SessionID: f.SessionID, Signature: sig, LastError: f.LastError, Calls: clampInt32(int64(f.Calls)),
	}))
}

// AddSelfUsage records one model call of memory's own (kind distil or embed) and its cost.
func (s *Store) AddSelfUsage(ctx context.Context, workspaceID, kind, model string, in, out int64, cost contract.MicroUSD) error {
	if !validID(workspaceID) {
		return contract.Invalid("workspaceId", "must be a workspace id")
	}
	return queryErr(s.q.InsertMemorySelfUsage(ctx, sqlc.InsertMemorySelfUsageParams{
		OrgID: orgID, WorkspaceID: workspaceID, Kind: kind, Model: model, InputTokens: max(in, 0), OutputTokens: max(out, 0), CostMicro: max(int64(cost), 0),
	}))
}

// SelfUsageSince is memory's own spend of the workspace since since.
func (s *Store) SelfUsageSince(ctx context.Context, workspaceID string, since time.Time) (contract.MicroUSD, error) {
	if !validID(workspaceID) {
		return 0, contract.Invalid("workspaceId", "must be a workspace id")
	}
	n, err := s.q.MemorySelfUsageSince(ctx, sqlc.MemorySelfUsageSinceParams{WorkspaceID: workspaceID, SinceTs: pgts(since)})
	return contract.MicroUSD(n), queryErr(err)
}

// WorkspaceCaptureAllowed reports the workspace's own capture setting: capture on and privacy mode not switched on for
// the workspace. The global PRIVACY_MODE is the caller's to apply (the strictest of the three wins). A workspace that
// is not the org's reports false.
func (s *Store) WorkspaceCaptureAllowed(ctx context.Context, workspaceID string) (bool, error) {
	if !validID(workspaceID) {
		return false, nil
	}
	r, err := s.q.MemoryWorkspaceCapture(ctx, sqlc.MemoryWorkspaceCaptureParams{OrgID: orgID, ID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, queryErr(err)
	}
	return r.Capture == "true" && r.PrivacyMode != "true", nil
}
