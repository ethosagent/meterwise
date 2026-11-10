package store

import (
	"context"
	"encoding/json"
	"sort"
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

func pgMicro(m *contract.MicroUSD) pgtype.Int8 {
	if m == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: int64(*m), Valid: true}
}
