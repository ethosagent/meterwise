package api

import (
	"context"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Ledger is what the control plane reads from the ledger tables; *store.Store implements it. The
// write side belongs to internal/ledger (rule H4): nothing here can change a row.
type Ledger interface {
	ListSessions(ctx context.Context, q contract.SessionQuery) (contract.SessionPage, error)
	Session(ctx context.Context, workspaceID, id string) (contract.SessionRecord, []contract.CallRecord, error)
	Stats(ctx context.Context, workspaceID string, from, to time.Time) (contract.Stats, error)
}

// registerLedger wires the Plan 05 read routes. The gateway has already authorized the workspace
// (from the path, or from the required workspaceId query for /api/sessions/{id}).
func (s *server) registerLedger() {
	register(s, "GET", "/api/workspaces/{id}/sessions", s.listSessions)
	register(s, "GET", "/api/sessions/{id}", s.getSession)
	register(s, "GET", "/api/workspaces/{id}/stats", s.workspaceStats)
}

func (s *server) ledger() (Ledger, error) {
	if s.Ledger == nil {
		return nil, contract.NewError(contract.CodeNotAvailable, "the ledger is not configured")
	}
	return s.Ledger, nil
}

func (s *server) listSessions(c Call, _ none) (contract.SessionList, error) {
	led, err := s.ledger()
	if err != nil {
		return contract.SessionList{}, err
	}
	q := contract.SessionQuery{WorkspaceID: c.Path("id"), Cursor: c.Query("cursor")}
	for _, p := range []struct {
		name string
		dst  *time.Time
	}{{"from", &q.From}, {"to", &q.To}} {
		if raw := c.Query(p.name); raw != "" {
			t, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return contract.SessionList{}, contract.Invalid(p.name, "must be an RFC 3339 time")
			}
			*p.dst = t
		}
	}
	if !q.From.IsZero() && !q.To.IsZero() && !q.From.Before(q.To) {
		return contract.SessionList{}, contract.Invalid("from", "must be before to")
	}
	limit, err := contract.ParsePageLimit(c.Query("limit"))
	if err != nil {
		return contract.SessionList{}, err
	}
	q.Limit = limit
	page, err := led.ListSessions(c.Ctx, q)
	if err != nil {
		return contract.SessionList{}, err
	}
	out := contract.SessionList{Items: make([]contract.SessionView, len(page.Items)), NextCursor: strOrNil(page.NextCursor)}
	for i, r := range page.Items {
		out.Items[i] = sessionView(r)
	}
	return out, nil
}

func (s *server) getSession(c Call, _ none) (contract.SessionDetail, error) {
	led, err := s.ledger()
	if err != nil {
		return contract.SessionDetail{}, err
	}
	// The gateway authorized session:read on workspaceId before this runs; the store reads
	// (workspace_id, id), so a session of another workspace is simply not found.
	sess, calls, err := led.Session(c.Ctx, c.Query("workspaceId"), c.Path("id"))
	if err != nil {
		return contract.SessionDetail{}, err
	}
	out := contract.SessionDetail{Session: sessionView(sess), Calls: make([]contract.CallView, len(calls))}
	for i, r := range calls {
		out.Calls[i] = callView(r)
	}
	return out, nil
}

func sessionView(r contract.SessionRecord) contract.SessionView {
	v := contract.SessionView{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Source: r.Source, StartedAt: rfc3339(r.StartedAt), LastSeenAt: rfc3339(r.LastSeenAt),
		Calls: r.Calls, InputTokens: r.InputTokens, OutputTokens: r.OutputTokens, CostUsd: r.Cost, SavedUsd: r.Saved,
		LikelySavedUsd: r.LikelySaved, Outcome: r.Outcome, Client: strOrNil(r.Client),
	}
	if r.ClosedAt != nil {
		closed := rfc3339(*r.ClosedAt)
		v.ClosedAt = &closed
	}
	return v
}

func callView(r contract.CallRecord) contract.CallView {
	tools := r.ToolCalls
	if tools == nil {
		tools = []string{}
	}
	return contract.CallView{
		ID: r.ID, CallIndex: r.CallIndex, StartedAt: rfc3339(r.StartedAt), Provider: r.Provider, Model: r.Model, Wire: r.Wire,
		Stream: r.Stream, Status: r.Status, Finish: r.Finish, InputTokens: r.Usage.Input, OutputTokens: r.Usage.Output,
		CachedTokens: r.Usage.Cached, ReasoningTokens: r.Usage.Reasoning, CostUsd: r.Cost, TtfbMs: r.TTFBMs, DurationMs: r.DurationMs,
		ToolCalls: tools, Blocked: r.Blocked, NotesInjected: r.NotesInjected,
	}
}

// statsRanges are the accepted ?range= values and their length in UTC days.
var statsRanges = map[string]int{"7d": 7, "30d": 30}

func (s *server) workspaceStats(c Call, _ none) (contract.StatsView, error) {
	led, err := s.ledger()
	if err != nil {
		return contract.StatsView{}, err
	}
	rng := c.Query("range")
	if rng == "" {
		rng = "7d"
	}
	days, ok := statsRanges[rng]
	if !ok {
		return contract.StatsView{}, contract.Invalid("range", "must be 7d or 30d")
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	today := now.UTC().Truncate(24 * time.Hour)
	from, to := today.AddDate(0, 0, -(days-1)), today.AddDate(0, 0, 1)
	st, err := led.Stats(c.Ctx, c.Path("id"), from, to)
	if err != nil {
		return contract.StatsView{}, err
	}
	have := make(map[string]contract.StatsDay, len(st.ByDay))
	for _, d := range st.ByDay {
		have[d.Day.UTC().Format(time.DateOnly)] = d
	}
	out := contract.StatsView{SpendUsd: st.Spend, Calls: st.Calls, Sessions: st.Sessions, ByDay: make([]contract.StatsDayView, 0, days)}
	if st.Sessions > 0 {
		out.AvgCallsPerSession = float64(st.Calls) / float64(st.Sessions)
	}
	for d := range days {
		key := from.AddDate(0, 0, d).Format(time.DateOnly)
		day := have[key]
		out.ByDay = append(out.ByDay, contract.StatsDayView{Day: key, SpendUsd: day.Spend, Calls: day.Calls, Sessions: day.Sessions})
	}
	return out, nil
}
