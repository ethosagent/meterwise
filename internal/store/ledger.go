package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// WriteResult is what one WriteCalls transaction did.
type WriteResult struct {
	Inserted   int // calls rows that were new
	Duplicates int // calls whose id already existed (a retry), left untouched and not counted again
}

// sessionAgg is the aggregate of the newly inserted calls of one session in one batch.
type sessionAgg struct {
	rec       contract.CallRecord // the latest call by end time: carries last finish and text
	lastSeen  time.Time
	started   time.Time
	calls     int
	in, out   int64
	costMicro int64
	priced    bool
	preview   string
	client    string
}

// WriteCalls writes a batch of completed calls in one transaction: INSERT calls ON CONFLICT (id)
// DO NOTHING, then one sessions upsert per session for the rows actually inserted, then one
// last_call_at touch per workspace. Replaying a batch changes nothing. It is the only writer of
// calls and sessions (H4); internal/ledger is its only caller.
func (s *Store) WriteCalls(ctx context.Context, recs []contract.CallRecord) (WriteResult, error) {
	if len(recs) == 0 {
		return WriteResult{}, nil
	}
	var res WriteResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		params := make([]sqlc.InsertCallParams, len(recs))
		for i, r := range recs {
			p, err := insertParams(r)
			if err != nil {
				return err
			}
			params[i] = p
		}
		inserted := make([]bool, len(recs))
		var firstErr error
		br := q.InsertCall(ctx, params)
		br.QueryRow(func(i int, _ string, err error) {
			switch {
			case err == nil:
				inserted[i] = true
			case err == pgx.ErrNoRows:
				// a repeat of a call id: nothing inserted, nothing to count
			case firstErr == nil:
				firstErr = err
			}
		})
		if err := br.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		if firstErr != nil {
			return firstErr
		}
		fresh := make([]contract.CallRecord, 0, len(recs))
		for i, r := range recs {
			if inserted[i] {
				fresh = append(fresh, r)
			}
		}
		res = WriteResult{Inserted: len(fresh), Duplicates: len(recs) - len(fresh)}
		return s.writeSessions(ctx, q, fresh)
	})
	if err != nil {
		return WriteResult{}, queryErr(err)
	}
	return res, nil
}

func insertParams(r contract.CallRecord) (sqlc.InsertCallParams, error) {
	tools := r.ToolCalls
	if tools == nil {
		tools = []string{}
	}
	tc, err := json.Marshal(tools)
	if err != nil {
		return sqlc.InsertCallParams{}, fail(ClassPool, "store: tool call names are not JSON")
	}
	return sqlc.InsertCallParams{
		ID: r.ID, OrgID: r.OrgID, WorkspaceID: r.WorkspaceID, SessionID: r.SessionID, CallIndex: int32(r.CallIndex),
		StartedAt: pgts(r.StartedAt), Provider: r.Provider, Model: r.Model, Wire: r.Wire, Stream: r.Stream, Status: int32(r.Status),
		Finish: r.Finish, InputTokens: r.Usage.Input, OutputTokens: r.Usage.Output, CachedTokens: r.Usage.Cached,
		ReasoningTokens: r.Usage.Reasoning, CostMicro: pgMicro(r.Cost), TtfbMs: clampInt32(r.TTFBMs), DurationMs: clampInt32(r.DurationMs),
		ToolCalls: tc, Blocked: r.Blocked, NotesInjected: int32(r.NotesInjected), Client: r.Client,
	}, nil
}

func pgMicro(m *contract.MicroUSD) pgtype.Int8 {
	if m == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: int64(*m), Valid: true}
}

func clampInt32(v int64) int32 {
	switch {
	case v > 1<<31-1:
		return 1<<31 - 1
	case v < 0:
		return 0
	}
	return int32(v)
}

// writeSessions folds the inserted calls into per-session aggregates and upserts each, then touches
// last_call_at once per workspace.
func (s *Store) writeSessions(ctx context.Context, q *sqlc.Queries, fresh []contract.CallRecord) error {
	type key struct{ ws, id string }
	aggs := map[key]*sessionAgg{}
	type wsTouch struct {
		org string
		at  time.Time
	}
	touches := map[string]wsTouch{}
	for _, r := range fresh {
		end := r.StartedAt.Add(time.Duration(r.DurationMs) * time.Millisecond)
		k := key{r.WorkspaceID, r.SessionID}
		a := aggs[k]
		if a == nil {
			a = &sessionAgg{rec: r, started: r.SessionStart, lastSeen: end}
			aggs[k] = a
		}
		if r.SessionStart.Before(a.started) {
			a.started = r.SessionStart
		}
		if !end.Before(a.lastSeen) {
			a.rec, a.lastSeen = r, end
		}
		a.calls++
		a.in += r.Usage.Input
		a.out += r.Usage.Output
		if r.Cost != nil {
			a.costMicro += int64(*r.Cost)
			a.priced = true
		}
		if a.preview == "" {
			a.preview = r.Preview
		}
		if a.client == "" {
			a.client = r.Client
		}
		t := touches[r.WorkspaceID]
		if end.After(t.at) {
			touches[r.WorkspaceID] = wsTouch{org: r.OrgID, at: end}
		}
	}
	keys := make([]key, 0, len(aggs))
	for k := range aggs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { // a stable lock order across concurrent batches
		return keys[i].ws+"/"+keys[i].id < keys[j].ws+"/"+keys[j].id
	})
	ups := make([]sqlc.UpsertSessionParams, 0, len(keys))
	for _, k := range keys {
		a := aggs[k]
		var cost *contract.MicroUSD
		if a.priced {
			c := contract.MicroUSD(a.costMicro)
			cost = &c
		}
		ups = append(ups, sqlc.UpsertSessionParams{
			ID: k.id, OrgID: a.rec.OrgID, WorkspaceID: k.ws, Source: a.rec.SessionSource, StartedAt: pgts(a.started),
			LastSeenAt: pgts(a.lastSeen), Calls: int32(a.calls), InputTokens: a.in, OutputTokens: a.out, CostMicro: pgMicro(cost),
			Client: a.client, Preview: a.preview, LastFinish: a.rec.Finish, LastHadText: a.rec.LastHadText,
		})
	}
	if len(ups) > 0 {
		br := q.UpsertSession(ctx, ups)
		var first error
		br.Exec(func(_ int, err error) {
			if err != nil && first == nil {
				first = err
			}
		})
		if err := br.Close(); err != nil && first == nil {
			first = err
		}
		if first != nil {
			return first
		}
	}
	wss := make([]string, 0, len(touches))
	for ws := range touches {
		wss = append(wss, ws)
	}
	sort.Strings(wss)
	for _, ws := range wss {
		t := touches[ws]
		if err := q.TouchWorkspaceLastCall(ctx, sqlc.TouchWorkspaceLastCallParams{At: pgts(t.at), WorkspaceID: ws, OrgID: t.org}); err != nil {
			return err
		}
	}
	return nil
}

// CloseSession finalises a quiet session: closed_at = at and the outcome heuristic (the last call
// finished ok with assistant text: completed; otherwise abandoned). It reports whether a row changed;
// an unknown or already closed session is not an error.
func (s *Store) CloseSession(ctx context.Context, workspaceID, sessionID string, at time.Time) (bool, error) {
	if !validID(workspaceID) {
		return false, nil
	}
	n, err := s.q.CloseSession(ctx, sqlc.CloseSessionParams{ClosedAt: pgts(at), WorkspaceID: workspaceID, ID: sessionID})
	return n > 0, queryErr(err)
}

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
	if !has {
		return nil
	}
	m := contract.MicroUSD(v)
	return &m
}

func microPtr(v pgtype.Int8) *contract.MicroUSD {
	if !v.Valid {
		return nil
	}
	m := contract.MicroUSD(v.Int64)
	return &m
}

// Session page sizes: DefaultSessionLimit unless the caller asks for fewer or more, never more than MaxSessionLimit.
const (
	DefaultSessionLimit = 50
	MaxSessionLimit     = 200
	maxSessionCalls     = 5000
)

var (
	farPast   = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	farFuture = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
)

// ListSessions returns one page of a workspace's sessions, newest first by (started_at, id). An invalid
// cursor, workspace id or limit is a validation error. The query is scoped by workspace_id.
func (s *Store) ListSessions(ctx context.Context, q contract.SessionQuery) (contract.SessionPage, error) {
	limit := q.Limit
	switch {
	case limit == 0:
		limit = DefaultSessionLimit
	case limit < 0 || limit > MaxSessionLimit:
		return contract.SessionPage{}, contract.Invalid("limit", "must be between 1 and "+strconv.Itoa(MaxSessionLimit))
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
	at, id, err := decodeSessionCursor(q.Cursor)
	if err != nil {
		return contract.SessionPage{}, err
	}
	var rows []sessionRow
	if q.Cursor == "" {
		first, err := s.q.ListSessionsFirst(ctx, sqlc.ListSessionsFirstParams{WorkspaceID: q.WorkspaceID, FromTs: pgts(from), ToTs: pgts(to), MaxRows: int32(limit + 1)})
		if err != nil {
			return contract.SessionPage{}, queryErr(err)
		}
		for _, r := range first {
			rows = append(rows, sessionRow(r))
		}
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
			page.NextCursor = encodeSessionCursor(last.StartedAt, last.ID)
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
		var tools []string
		_ = json.Unmarshal(c.ToolCalls, &tools)
		if tools == nil {
			tools = []string{}
		}
		out[i] = contract.CallRecord{
			ID: c.ID, WorkspaceID: workspaceID, SessionID: id, CallIndex: int(c.CallIndex), StartedAt: ts(c.StartedAt), Provider: c.Provider,
			Model: c.Model, Wire: c.Wire, Stream: c.Stream, Status: int(c.Status), Finish: c.Finish,
			Usage: contract.Usage{Input: c.InputTokens, Output: c.OutputTokens, Cached: c.CachedTokens, Reasoning: c.ReasoningTokens},
			Cost:  microIf(c.HasCost, c.CostMicro), TTFBMs: int64(c.TtfbMs), DurationMs: int64(c.DurationMs), ToolCalls: tools, Blocked: c.Blocked,
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

// The session cursor is "<unix micro>.<id>" in URL-safe base64: opaque to clients.
func encodeSessionCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d.%s", at.UnixMicro(), id)))
}

func decodeSessionCursor(c string) (time.Time, string, error) {
	if c == "" {
		return time.Time{}, "", nil
	}
	bad := contract.Invalid("cursor", "is not a cursor this API issued")
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, "", bad
	}
	us, id, ok := strings.Cut(string(raw), ".")
	micro, err := strconv.ParseInt(us, 10, 64)
	if !ok || err != nil || id == "" {
		return time.Time{}, "", bad
	}
	return time.UnixMicro(micro).UTC(), id, nil
}
