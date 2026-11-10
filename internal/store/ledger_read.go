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

// ListSessions returns one page of a workspace's sessions, newest first by (started_at, id), optionally only
// the sessions of one client (as stored; "unknown" is a NULL client) or outcome. An invalid cursor, workspace id,
// limit or outcome is a validation error. The query is scoped by workspace_id.
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
	if !contract.ValidSessionOutcome(q.Outcome) {
		return contract.SessionPage{}, contract.Invalid("outcome", "must be completed, abandoned or open")
	}
	if !contract.ValidSessionTag(q.Tag) {
		return contract.SessionPage{}, contract.Invalid("tag", "must be trimmed, routed or remembered")
	}
	at, id, err := decodeCursor(q.Cursor)
	if err != nil {
		return contract.SessionPage{}, err
	}
	params := sqlc.ListSessionsParams{WorkspaceID: q.WorkspaceID, FromTs: pgts(from), ToTs: pgts(to), MaxRows: clampInt32(int64(limit + 1))}
	params.Client, params.Outcome, params.Tag = textArg(q.Client), textArg(q.Outcome), textArg(q.Tag)
	if q.Cursor != "" {
		params.CursorAt, params.CursorID = pgts(at), pgtype.Text{String: id, Valid: true}
	}
	rows, err := s.q.ListSessions(ctx, params)
	if err != nil {
		return contract.SessionPage{}, queryErr(err)
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

// enumPtr is a nullable text column as a pointer to its string type.
func enumPtr[T ~string](v pgtype.Text) *T {
	if !v.Valid {
		return nil
	}
	e := T(v.String)
	return &e
}

func intPtr(v pgtype.Int4) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int32)
	return &n
}

// textArg is a nullable text parameter: "" means no filter.
func textArg(v string) pgtype.Text { return pgtype.Text{String: v, Valid: v != ""} }

// sessionRow is the shape every session query returns.
type sessionRow = sqlc.ListSessionsRow

func sessionOf(r sessionRow) contract.SessionRecord {
	return contract.SessionRecord{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Source: r.Source, StartedAt: ts(r.StartedAt), LastSeenAt: ts(r.LastSeenAt),
		ClosedAt: tsp(r.ClosedAt), Calls: int(r.Calls), InputTokens: r.InputTokens, OutputTokens: r.OutputTokens,
		Cost: microIf(r.HasCost, r.CostMicro), Saved: contract.MicroUSD(r.SavedMicro), LikelySaved: contract.MicroUSD(r.LikelySavedMicro),
		Outcome: r.Outcome, Client: r.Client, TrimTokens: r.TrimTokens, RoutedCalls: int(r.RoutedCalls), Hints: int(r.Hints),
	}
}

// Session returns one session of a workspace and its calls in call order (at most 5000; truncated says
// there were more). A session that does not exist in that workspace is not found, whoever asks.
func (s *Store) Session(ctx context.Context, workspaceID, id string) (sess contract.SessionRecord, out []contract.CallRecord, truncated bool, err error) {
	return s.sessionWith(ctx, s.q, workspaceID, id)
}

// SessionDetail reads a session, its calls and its monitor events from one REPEATABLE READ, read-only
// snapshot, so a call that lands between the reads can never show up in the events without its call row.
func (s *Store) SessionDetail(ctx context.Context, workspaceID, id string) (sess contract.SessionRecord, calls []contract.CallRecord, truncated bool, events []contract.MonitorEventRecord, err error) {
	err = s.readSnapshot(ctx, func(q *sqlc.Queries) error {
		var err error
		if sess, calls, truncated, err = s.sessionWith(ctx, q, workspaceID, id); err != nil {
			return err
		}
		events, err = s.eventsWith(ctx, q, workspaceID, id)
		return err
	})
	if err != nil {
		return sess, nil, false, nil, err // readSnapshot passes a typed error from the reads through unchanged
	}
	return sess, calls, truncated, events, nil
}

func (s *Store) sessionWith(ctx context.Context, q *sqlc.Queries, workspaceID, id string) (sess contract.SessionRecord, out []contract.CallRecord, truncated bool, err error) {
	if !validID(workspaceID) {
		return sess, nil, false, contract.Invalid("workspaceId", "must be a workspace id")
	}
	r, err := q.GetSession(ctx, sqlc.GetSessionParams{WorkspaceID: workspaceID, ID: id})
	if err != nil {
		return sess, nil, false, queryErr(err)
	}
	calls, err := q.ListSessionCalls(ctx, sqlc.ListSessionCallsParams{WorkspaceID: workspaceID, SessionID: id, MaxRows: maxSessionCalls + 1})
	if err != nil {
		return sess, nil, false, queryErr(err)
	}
	if truncated = len(calls) > maxSessionCalls; truncated {
		calls = calls[:maxSessionCalls]
	}
	out = make([]contract.CallRecord, len(calls))
	for i, c := range calls {
		out[i] = contract.CallRecord{
			ID: c.ID, WorkspaceID: workspaceID, SessionID: id, CallIndex: int(c.CallIndex), StartedAt: ts(c.StartedAt), Provider: c.Provider,
			Model: c.Model, Wire: c.Wire, Stream: c.Stream, Status: int(c.Status), Finish: c.Finish,
			Usage: contract.Usage{Input: c.InputTokens, Output: c.OutputTokens, Cached: c.CachedTokens, Reasoning: c.ReasoningTokens},
			Cost:  microIf(c.HasCost, c.CostMicro), TTFBMs: int64(c.TtfbMs), DurationMs: int64(c.DurationMs), ToolCalls: s.toolNames(c.ID, c.ToolCalls), Blocked: c.Blocked,
			NotesInjected: int(c.NotesInjected),
			TrimTokens:    c.TrimTokens, SavedUSDTrim: contract.MicroUSD(c.SavedTrimMicro), SavedUSDRouter: contract.MicroUSD(c.SavedRouterMicro),
			HintInjected: c.HintInjected, RoutedFrom: c.RoutedFrom.String, RoutedTo: c.RoutedTo.String,
			TrimAction: enumPtr[contract.TrimAction](c.TrimAction), TrimConfidence: enumPtr[contract.Confidence](c.TrimConfidence),
			RouterAction: enumPtr[contract.RouteAction](c.RouterAction), RouterScoreBp: intPtr(c.RouterScoreBp),
		}
	}
	return sessionOf(sessionRow(r)), out, truncated, nil
}

// Stats sums a workspace's priced spend, calls and distinct sessions over [from, to) and per UTC
// day (only days with calls are returned; the caller zero-fills), plus the two savings numbers of the
// range (saved and likely, separate: never summed).
func (s *Store) Stats(ctx context.Context, workspaceID string, from, to time.Time) (contract.Stats, error) {
	if !validID(workspaceID) {
		return contract.Stats{}, contract.Invalid("workspaceId", "must be a workspace id")
	}
	rows, err := s.q.Stats(ctx, sqlc.StatsParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)})
	if err != nil {
		return contract.Stats{}, queryErr(err)
	}
	var st contract.Stats
	for _, r := range rows {
		if !r.Day.Valid { // the totals row
			st.Spend, st.Calls, st.Sessions = contract.MicroUSD(r.SpendMicro), int(r.Calls), int(r.Sessions)
			continue
		}
		st.ByDay = append(st.ByDay, contract.StatsDay{Day: r.Day.Time.UTC(), Spend: contract.MicroUSD(r.SpendMicro), Calls: int(r.Calls), Sessions: int(r.Sessions)})
	}
	sv, err := s.q.MonitorSavedTotals(ctx, sqlc.MonitorSavedTotalsParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)})
	if err != nil {
		return contract.Stats{}, queryErr(err)
	}
	st.Saved, st.Likely = contract.MicroUSD(sv.SavedMicro), contract.MicroUSD(sv.LikelyMicro)
	cs, err := s.q.SavingsCallSavers(ctx, sqlc.SavingsCallSaversParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)})
	if err != nil {
		return contract.Stats{}, queryErr(err)
	}
	for _, c := range cs { // trim and router money is in the same two numbers as the monitors' (ADR 0027)
		if !c.Day.Valid {
			st.Saved += contract.MicroUSD(c.TrimSavedMicro + c.RouterSavedMicro)
			st.Likely += contract.MicroUSD(c.TrimLikelyMicro)
		}
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
