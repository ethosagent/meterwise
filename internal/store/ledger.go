package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
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
	payload, err := json.Marshal(callJSONs(recs))
	if err != nil {
		return WriteResult{}, fail(ClassPool, "store: calls are not JSON")
	}
	var res WriteResult
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		ids, err := q.InsertCalls(ctx, payload)
		if err != nil {
			return err
		}
		got := make(map[string]bool, len(ids))
		for _, id := range ids {
			got[id] = true
		}
		fresh := make([]contract.CallRecord, 0, len(ids))
		for _, r := range recs {
			if got[r.ID] { // a duplicate id inside the batch is inserted once: count its first occurrence only
				fresh = append(fresh, r)
				got[r.ID] = false
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

// callJSON is one element of the InsertCalls array. Cost is omitted for an unpriced call.
type callJSON struct {
	ID            string   `json:"id"`
	OrgID         string   `json:"orgId"`
	WorkspaceID   string   `json:"workspaceId"`
	SessionID     string   `json:"sessionId"`
	CallIndex     int32    `json:"callIndex"`
	StartedUs     int64    `json:"startedUs"`
	Provider      string   `json:"provider"`
	Model         string   `json:"model"`
	Wire          string   `json:"wire"`
	Stream        bool     `json:"stream"`
	Status        int32    `json:"status"`
	Finish        string   `json:"finish"`
	Input         int64    `json:"input"`
	Output        int64    `json:"output"`
	Cached        int64    `json:"cached"`
	Reasoning     int64    `json:"reasoning"`
	Cost          *int64   `json:"cost,omitempty"`
	TTFBMs        int32    `json:"ttfbMs"`
	DurationMs    int32    `json:"durationMs"`
	ToolCalls     []string `json:"toolCalls"`
	Blocked       bool     `json:"blocked"`
	NotesInjected int32    `json:"notesInjected"`
	Client        string   `json:"client"`
}

func callJSONs(recs []contract.CallRecord) []callJSON {
	out := make([]callJSON, len(recs))
	for i, r := range recs {
		tools := r.ToolCalls
		if tools == nil {
			tools = []string{}
		}
		var cost *int64
		if r.Cost != nil {
			c := int64(*r.Cost)
			cost = &c
		}
		out[i] = callJSON{
			ID: r.ID, OrgID: r.OrgID, WorkspaceID: r.WorkspaceID, SessionID: r.SessionID, CallIndex: clampInt32(int64(r.CallIndex)),
			StartedUs: r.StartedAt.UnixMicro(), Provider: r.Provider, Model: r.Model, Wire: r.Wire, Stream: r.Stream,
			Status: clampInt32(int64(r.Status)), Finish: r.Finish, Input: r.Usage.Input, Output: r.Usage.Output, Cached: r.Usage.Cached,
			Reasoning: r.Usage.Reasoning, Cost: cost, TTFBMs: clampInt32(r.TTFBMs), DurationMs: clampInt32(r.DurationMs), ToolCalls: tools,
			Blocked: r.Blocked, NotesInjected: clampInt32(int64(r.NotesInjected)), Client: r.Client,
		}
	}
	return out
}

func pgMicro(m *contract.MicroUSD) pgtype.Int8 {
	if m == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: int64(*m), Valid: true}
}

func clampInt32(v int64) int32 {
	switch {
	case v > math.MaxInt32:
		return math.MaxInt32
	case v < 0:
		return 0
	}
	return int32(v)
}

// sessionKey identifies a session across workspaces.
type sessionKey struct{ ws, id string }

// foldSessions aggregates the inserted calls per session and finds each workspace's newest call end.
func foldSessions(fresh []contract.CallRecord) (map[sessionKey]*sessionAgg, map[string]wsTouch) {
	aggs := map[sessionKey]*sessionAgg{}
	touches := map[string]wsTouch{}
	for _, r := range fresh {
		end := r.StartedAt.Add(time.Duration(r.DurationMs) * time.Millisecond)
		k := sessionKey{r.WorkspaceID, r.SessionID}
		a := aggs[k]
		if a == nil {
			a = &sessionAgg{rec: r, started: r.SessionStart, lastSeen: end}
			aggs[k] = a
		}
		a.add(r, end)
		if t := touches[r.WorkspaceID]; end.After(t.at) {
			touches[r.WorkspaceID] = wsTouch{org: r.OrgID, at: end}
		}
	}
	return aggs, touches
}

type wsTouch struct {
	org string
	at  time.Time
}

func (a *sessionAgg) add(r contract.CallRecord, end time.Time) {
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
}

func (a *sessionAgg) params(k sessionKey) sqlc.UpsertSessionParams {
	var cost *contract.MicroUSD
	if a.priced {
		c := contract.MicroUSD(a.costMicro)
		cost = &c
	}
	return sqlc.UpsertSessionParams{
		ID: k.id, OrgID: a.rec.OrgID, WorkspaceID: k.ws, Source: a.rec.SessionSource, StartedAt: pgts(a.started),
		LastSeenAt: pgts(a.lastSeen), Calls: clampInt32(int64(a.calls)), InputTokens: a.in, OutputTokens: a.out, CostMicro: pgMicro(cost),
		Client: a.client, Preview: a.preview, LastFinish: a.rec.Finish, LastHadText: a.rec.LastHadText,
	}
}

// writeSessions upserts one row per session of the inserted calls, then touches last_call_at once per
// workspace. Keys are sorted so concurrent batches lock rows in the same order.
func (s *Store) writeSessions(ctx context.Context, q *sqlc.Queries, fresh []contract.CallRecord) error {
	aggs, touches := foldSessions(fresh)
	keys := make([]sessionKey, 0, len(aggs))
	for k := range aggs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].ws+"/"+keys[i].id < keys[j].ws+"/"+keys[j].id })
	for _, k := range keys {
		if err := q.UpsertSession(ctx, aggs[k].params(k)); err != nil {
			return err
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
	MaxSessionLimit     = contract.MaxPageLimit
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
	case limit < contract.MinPageLimit || limit > MaxSessionLimit:
		return contract.SessionPage{}, contract.Invalid("limit", contract.LimitMessage)
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

// UnreadableToolCalls is how many stored tool_calls values could not be read since Open.
func (s *Store) UnreadableToolCalls() int64 { return s.badTools.Load() }

// SetLogger sets the logger the store reports data problems to (the app logger). Until it is set they
// are counted but not logged.
func (s *Store) SetLogger(l *slog.Logger) { s.log.Store(l) }

func (s *Store) logger() *slog.Logger {
	if l := s.log.Load(); l != nil {
		return l
	}
	return slog.New(slog.DiscardHandler)
}

// toolNames decodes a calls.tool_calls value. One that is not a JSON array of names reads as no tool
// calls, is counted every time and is logged the first time, so a corrupt row leaves a trace.
func (s *Store) toolNames(callID string, raw []byte) []string {
	tools := []string{}
	if err := json.Unmarshal(raw, &tools); err != nil {
		s.badTools.Add(1)
		s.badToolsOnce.Do(func() {
			s.logger().Warn("store: calls.tool_calls is not a JSON array of names; read as none (counted, logged once)",
				slog.String("call_id", callID))
		})
		return []string{}
	}
	if tools == nil {
		tools = []string{}
	}
	return tools
}
