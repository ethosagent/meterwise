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
	// SessionDetail reads the session, its calls and its monitor events from one snapshot.
	SessionDetail(ctx context.Context, workspaceID, id string) (sess contract.SessionRecord, calls []contract.CallRecord, truncated bool, events []contract.MonitorEventRecord, err error)
	Stats(ctx context.Context, workspaceID string, from, to time.Time) (contract.Stats, error)
	MonitorStats(ctx context.Context, workspaceID string, from, to time.Time) ([]contract.MonitorStatRow, error)
}

// maxClientFilter bounds the ?client= filter (a family is at most 64 bytes).
const maxClientFilter = 64

// registerLedger wires the Plan 05 read routes. The gateway has already authorized the workspace
// (from the path, or from the required workspaceId query for /api/sessions/{id}).
func (s *server) registerLedger() {
	register(s, "GET", "/api/workspaces/{id}/sessions", s.listSessions)
	register(s, "GET", "/api/sessions/{id}", s.getSession)
	register(s, "GET", "/api/workspaces/{id}/stats", s.workspaceStats)
}

func (s *server) ledger() (Ledger, error) {
	if s.Ledger == nil {
		return nil, contract.Unavailable("the ledger", "")
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
	if q.Client, q.Outcome = c.Query("client"), c.Query("outcome"); len(q.Client) > maxClientFilter {
		return contract.SessionList{}, contract.Invalid("client", "is too long")
	} else if !contract.ValidSessionOutcome(q.Outcome) {
		return contract.SessionList{}, contract.Invalid("outcome", "must be completed, abandoned or open")
	}
	if q.Tag = c.Query("tag"); !contract.ValidSessionTag(q.Tag) {
		return contract.SessionList{}, contract.Invalid("tag", "must be trimmed, routed or remembered")
	}
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
	sess, calls, truncated, events, err := led.SessionDetail(c.Ctx, c.Query("workspaceId"), c.Path("id"))
	if err != nil {
		return contract.SessionDetail{}, err
	}
	out := contract.SessionDetail{
		Session: sessionView(sess), Calls: make([]contract.CallView, len(calls)), Truncated: truncated,
		MonitorEvents: make([]contract.MonitorEventView, len(events)), MemoryHits: []contract.MemoryHitView{},
	}
	for i, r := range calls {
		out.Calls[i] = callView(r)
	}
	for i, e := range events {
		out.MonitorEvents[i] = e.View()
	}
	out.Insight = insightOf(events)
	return out, nil
}

func sessionView(r contract.SessionRecord) contract.SessionView {
	v := contract.SessionView{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Source: r.Source, StartedAt: contract.RFC3339(r.StartedAt), LastSeenAt: contract.RFC3339(r.LastSeenAt),
		Calls: r.Calls, InputTokens: r.InputTokens, OutputTokens: r.OutputTokens, CostUsd: r.Cost, SavedUsd: r.Saved,
		LikelySavedUsd: r.LikelySaved, Outcome: r.Outcome, Client: strOrNil(r.Client),
		TrimTokens: r.TrimTokens, RoutedCalls: r.RoutedCalls, Hints: r.Hints,
	}
	if r.ClosedAt != nil {
		closed := contract.RFC3339(*r.ClosedAt)
		v.ClosedAt = &closed
	}
	return v
}

func callView(r contract.CallRecord) contract.CallView {
	return contract.CallView{
		ID: r.ID, CallIndex: r.CallIndex, StartedAt: contract.RFC3339(r.StartedAt), Provider: r.Provider, Model: r.Model, Wire: r.Wire,
		Stream: r.Stream, Status: r.Status, Finish: r.Finish, InputTokens: r.Usage.Input, OutputTokens: r.Usage.Output,
		CachedTokens: r.Usage.Cached, ReasoningTokens: r.Usage.Reasoning, CostUsd: r.Cost, TtfbMs: r.TTFBMs, DurationMs: r.DurationMs,
		ToolCalls: r.ToolCalls, Blocked: r.Blocked, NotesInjected: r.NotesInjected,
		TrimAction: strEnum(r.TrimAction), TrimConfidence: strEnum(r.TrimConfidence), TrimTokens: r.TrimTokens, SavedUsdTrim: r.SavedUSDTrim,
		RouterAction: strEnum(r.RouterAction), RoutedFrom: strOrNil(r.RoutedFrom), RoutedTo: strOrNil(r.RoutedTo), RouterScoreBp: r.RouterScoreBp,
		SavedUsdRouter: r.SavedUSDRouter, HintInjected: r.HintInjected,
	}
}

// strEnum is a nullable enum as a nullable string.
func strEnum[T ~string](v *T) *string {
	if v == nil {
		return nil
	}
	s := string(*v)
	return &s
}

// statsRanges are the accepted ?range= values and their length in UTC days.
var statsRanges = map[string]int{"7d": 7, "30d": 30}

// now is the server's clock (Options.Now in tests, time.Now otherwise): every handler reads time here.
func (s *server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// statsWindow is the [from, to) UTC-day window of a range of days ending with today.
func statsWindow(now time.Time, days int) (from, to time.Time) {
	today := contract.UTCDayStart(now)
	return today.AddDate(0, 0, -(days - 1)), today.AddDate(0, 0, 1)
}

// fillDays turns the days that had calls into one entry per day of the window, zero-filled.
func fillDays(from time.Time, days int, have []contract.StatsDay) []contract.StatsDayView {
	byDay := make(map[string]contract.StatsDay, len(have))
	for _, d := range have {
		byDay[d.Day.UTC().Format(time.DateOnly)] = d
	}
	out := make([]contract.StatsDayView, 0, days)
	for d := range days {
		key := from.AddDate(0, 0, d).Format(time.DateOnly)
		day := byDay[key]
		out = append(out, contract.StatsDayView{Day: key, SpendUsd: day.Spend, Calls: day.Calls, Sessions: day.Sessions})
	}
	return out
}

// statsRangeOf reads ?range= (7d or 30d, default 7d) into its name, length in days and [from, to) window. An
// unknown value is a 422 naming the range field. Workspace stats and monitor stats share it.
func (s *server) statsRangeOf(c Call) (rng string, days int, from, to time.Time, err error) {
	rng = c.Query("range")
	if rng == "" {
		rng = "7d"
	}
	days, ok := statsRanges[rng]
	if !ok {
		return "", 0, from, to, contract.Invalid("range", "must be 7d or 30d")
	}
	from, to = statsWindow(s.now(), days)
	return rng, days, from, to, nil
}

func (s *server) workspaceStats(c Call, _ none) (contract.StatsView, error) {
	led, err := s.ledger()
	if err != nil {
		return contract.StatsView{}, err
	}
	_, days, from, to, err := s.statsRangeOf(c)
	if err != nil {
		return contract.StatsView{}, err
	}
	st, err := led.Stats(c.Ctx, c.Path("id"), from, to)
	if err != nil {
		return contract.StatsView{}, err
	}
	out := contract.StatsView{
		SpendUsd: st.Spend, Calls: st.Calls, Sessions: st.Sessions, ByDay: fillDays(from, days, st.ByDay),
		SavedUsd: st.Saved, LikelySavedUsd: st.Likely, // two numbers, never a total
	}
	if st.Sessions > 0 {
		out.AvgCallsPerSession = float64(st.Calls) / float64(st.Sessions)
	}
	return out, nil
}
