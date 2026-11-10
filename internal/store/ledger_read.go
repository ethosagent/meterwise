package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// Pricing returns every price row for the savings snapshot.
func (s *Store) Pricing(ctx context.Context) ([]contract.PriceRow, error) {
	rows, err := s.q.ListPricing(ctx)
	if err != nil {
		return nil, queryErr(err)
	}
	out := make([]contract.PriceRow, len(rows))
	for i, r := range rows {
		out[i] = contract.PriceRow{
			Provider: r.Provider, Model: r.Model, InputPerM: contract.MicroUSD(r.InputPerM), OutputPerM: contract.MicroUSD(r.OutputPerM),
			CachedPerM: microPtr(r.CachedPerM), ReasoningPerM: microPtr(r.ReasoningPerM), EffectiveFrom: ts(r.EffectiveFrom),
		}
	}
	return out, nil
}

func microIf(has bool, v int64) *contract.MicroUSD {
	return microPtr(pgtype.Int8{Int64: v, Valid: has})
}

func microPtr(v pgtype.Int8) *contract.MicroUSD {
	if !v.Valid {
		return nil
	}
	m := contract.MicroUSD(v.Int64)
	return &m
}

const maxSessionCalls = 5000

var (
	farPast   = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	farFuture = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
)

// ListSessions returns one page of a workspace's sessions, newest first by (started_at, id). An invalid
// cursor, workspace id or limit is a validation error. The query is scoped by workspace_id.
func (s *Store) ListSessions(ctx context.Context, q contract.SessionQuery) (contract.SessionPage, error) {
	limit, err := contract.ResolvePageLimit(q.Limit)
	if err != nil {
		return contract.SessionPage{}, err
	}
	if !validID(q.WorkspaceID) {
		return contract.SessionPage{}, contract.Invalid("workspaceId", "must be a workspace id")
	}
	from, to := q.From, q.To
	if from.IsZero() {
		from = farPast
	}
	if to.IsZero() {
		to = farFuture
	}
	at, id, err := decodeCursor(q.Cursor)
	if err != nil {
		return contract.SessionPage{}, err
	}
	var rows []sessionRow
	if q.Cursor == "" {
		first, err := s.q.ListSessionsFirst(ctx, sqlc.ListSessionsFirstParams{WorkspaceID: q.WorkspaceID, FromTs: pgts(from), ToTs: pgts(to), MaxRows: int32(limit + 1)})
		if err != nil {
			return contract.SessionPage{}, queryErr(err)
		}
		rows = append(rows, first...)
	} else {
		after, err := s.q.ListSessionsAfter(ctx, sqlc.ListSessionsAfterParams{WorkspaceID: q.WorkspaceID, FromTs: pgts(from), ToTs: pgts(to), CursorAt: pgts(at), CursorID: id, MaxRows: int32(limit + 1)})
		if err != nil {
			return contract.SessionPage{}, queryErr(err)
		}
		for _, r := range after {
			rows = append(rows, sessionRow(r))
		}
	}
	page := contract.SessionPage{Items: make([]contract.SessionRecord, 0, min(len(rows), limit))}
	for i, r := range rows {
		if i == limit { // the extra row only proves there is a next page
			last := page.Items[limit-1]
			page.NextCursor = encodeCursor(last.StartedAt, last.ID)
			break
		}
		page.Items = append(page.Items, sessionOf(r))
	}
	return page, nil
}

// sessionRow is the shape every session query returns.
type sessionRow = sqlc.ListSessionsFirstRow

func sessionOf(r sessionRow) contract.SessionRecord {
	return contract.SessionRecord{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Source: r.Source, StartedAt: ts(r.StartedAt), LastSeenAt: ts(r.LastSeenAt),
		ClosedAt: tsp(r.ClosedAt), Calls: int(r.Calls), InputTokens: r.InputTokens, OutputTokens: r.OutputTokens,
		Cost: microIf(r.HasCost, r.CostMicro), Saved: contract.MicroUSD(r.SavedMicro), LikelySaved: contract.MicroUSD(r.LikelySavedMicro),
		Outcome: r.Outcome, Client: r.Client,
	}
}

// Session returns one session of a workspace and its calls in call order (at most 5000). A session
// that does not exist in that workspace is not found, whoever asks.
func (s *Store) Session(ctx context.Context, workspaceID, id string) (contract.SessionRecord, []contract.CallRecord, error) {
	if !validID(workspaceID) {
		return contract.SessionRecord{}, nil, contract.Invalid("workspaceId", "must be a workspace id")
	}
	r, err := s.q.GetSession(ctx, sqlc.GetSessionParams{WorkspaceID: workspaceID, ID: id})
	if err != nil {
		return contract.SessionRecord{}, nil, queryErr(err)
	}
	calls, err := s.q.ListSessionCalls(ctx, sqlc.ListSessionCallsParams{WorkspaceID: workspaceID, SessionID: id, MaxRows: maxSessionCalls})
	if err != nil {
		return contract.SessionRecord{}, nil, queryErr(err)
	}
	out := make([]contract.CallRecord, len(calls))
	for i, c := range calls {
		out[i] = contract.CallRecord{
			ID: c.ID, WorkspaceID: workspaceID, SessionID: id, CallIndex: int(c.CallIndex), StartedAt: ts(c.StartedAt), Provider: c.Provider,
			Model: c.Model, Wire: c.Wire, Stream: c.Stream, Status: int(c.Status), Finish: c.Finish,
			Usage: contract.Usage{Input: c.InputTokens, Output: c.OutputTokens, Cached: c.CachedTokens, Reasoning: c.ReasoningTokens},
			Cost:  microIf(c.HasCost, c.CostMicro), TTFBMs: int64(c.TtfbMs), DurationMs: int64(c.DurationMs), ToolCalls: s.toolNames(c.ID, c.ToolCalls), Blocked: c.Blocked,
			NotesInjected: int(c.NotesInjected),
		}
	}
	return sessionOf(sessionRow(r)), out, nil
}

// Stats sums a workspace's priced spend, calls and distinct sessions over [from, to) and per UTC
// day (only days with calls are returned; the caller zero-fills).
func (s *Store) Stats(ctx context.Context, workspaceID string, from, to time.Time) (contract.Stats, error) {
	if !validID(workspaceID) {
		return contract.Stats{}, contract.Invalid("workspaceId", "must be a workspace id")
	}
	tot, err := s.q.StatsTotals(ctx, sqlc.StatsTotalsParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)})
	if err != nil {
		return contract.Stats{}, queryErr(err)
	}
	days, err := s.q.StatsByDay(ctx, sqlc.StatsByDayParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)})
	if err != nil {
		return contract.Stats{}, queryErr(err)
	}
	st := contract.Stats{Spend: contract.MicroUSD(tot.SpendMicro), Calls: int(tot.Calls), Sessions: int(tot.Sessions)}
	for _, d := range days {
		st.ByDay = append(st.ByDay, contract.StatsDay{Day: d.Day.Time.UTC(), Spend: contract.MicroUSD(d.SpendMicro), Calls: int(d.Calls), Sessions: int(d.Sessions)})
	}
	return st, nil
}

// CallStatus reads the first call time and the count of calls since since (the UTC day start).
func (s *Store) CallStatus(ctx context.Context, workspaceID string, since time.Time) (contract.CallStatus, error) {
	r, err := s.q.CallStatus(ctx, sqlc.CallStatusParams{WorkspaceID: workspaceID, SinceTs: pgts(since)})
	if err != nil {
		return contract.CallStatus{}, queryErr(err)
	}
	return contract.CallStatus{FirstCallAt: tsp(r.FirstCallAt), CallsToday: int(r.CallsToday)}, nil
}
