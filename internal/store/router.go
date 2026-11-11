package store

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// RouterRows reads the router's static tables once, at boot: the default route pairs and the newest capability row
// of every model. router.BuildTables turns them, with a pricer, into the hook's tables.
func (s *Store) RouterRows(ctx context.Context) ([]contract.RoutePair, []contract.RouterCapRow, error) {
	routes, err := s.q.RouterRoutes(ctx)
	if err != nil {
		return nil, nil, queryErr(err)
	}
	caps, err := s.q.RouterCaps(ctx)
	if err != nil {
		return nil, nil, queryErr(err)
	}
	pairs := make([]contract.RoutePair, len(routes))
	for i, r := range routes {
		pairs[i] = contract.RoutePair{Provider: r.Provider, From: r.FromModel, To: r.ToModel}
	}
	rows := make([]contract.RouterCapRow, len(caps))
	for i, c := range caps {
		rows[i] = contract.RouterCapRow{Provider: c.Provider, Model: c.Model, JSONSchema: c.SupportsJsonSchema, Vision: c.SupportsVision, Reasoning: c.SupportsReasoning}
		if c.MaxTools.Valid {
			rows[i].MaxTools = new(int(c.MaxTools.Int32))
		}
	}
	return pairs, rows, nil
}

// RouterReader is the control plane's view of the router (api.RouterReader): the priced default pairs the process
// loaded at boot, and workspace-scoped suggestions, stats and feedback read from the database.
type RouterReader struct {
	s        *Store
	defaults []contract.RoutePair
}

// NewRouterReader builds the reader. defaults are the pairs router.BuildTables kept (priced, with capabilities).
func NewRouterReader(s *Store, defaults []contract.RoutePair) *RouterReader {
	return &RouterReader{s: s, defaults: slices.Clone(defaults)}
}

// Defaults are the priced default pairs.
func (r *RouterReader) Defaults(context.Context) ([]contract.RoutePair, error) {
	return slices.Clone(r.defaults), nil
}

// Suggestions are the default pairs whose source model the workspace's calls used in the last 30 days (a dated id
// counts for its base name), busiest model first. A model with no default pair suggests nothing.
func (r *RouterReader) Suggestions(ctx context.Context, workspaceID string) ([]contract.RoutePair, error) {
	if !validID(workspaceID) {
		return nil, contract.Invalid("workspaceId", "must be a workspace id")
	}
	seen, err := r.s.q.RouterSeenModels(ctx, workspaceID)
	if err != nil {
		return nil, queryErr(err)
	}
	var out []contract.RoutePair
	for _, m := range seen {
		for _, p := range r.defaults {
			if p.Provider == m.Provider && contract.ModelMatches(p.From, m.Model) && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

// Stats reads what the router did in the range, on one snapshot so the numbers agree. Mode is the caller's to fill.
func (r *RouterReader) Stats(ctx context.Context, workspaceID string, sr contract.SavingsRange) (contract.RouterStats, error) {
	if !validID(workspaceID) {
		return contract.RouterStats{}, contract.Invalid("workspaceId", "must be a workspace id")
	}
	from, to := window(sr)
	var (
		calls sqlc.RouterCallStatsRow
		sess  sqlc.RouterSessionStatsRow
		pairs []sqlc.RouterPairStatsRow
		worse int64
	)
	err := r.s.readSnapshot(ctx, func(q *sqlc.Queries) (err error) {
		if calls, err = q.RouterCallStats(ctx, sqlc.RouterCallStatsParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)}); err != nil {
			return err
		}
		if sess, err = q.RouterSessionStats(ctx, sqlc.RouterSessionStatsParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)}); err != nil {
			return err
		}
		if pairs, err = q.RouterPairStats(ctx, sqlc.RouterPairStatsParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)}); err != nil {
			return err
		}
		worse, err = q.RouterWorseCount(ctx, sqlc.RouterWorseCountParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)})
		return err
	})
	if err != nil {
		return contract.RouterStats{}, err
	}
	out := contract.RouterStats{
		Range: sr.Range, From: sr.FromDay(), To: sr.ToDay(), Calls: calls.Calls, RoutedCalls: calls.Routed, ObservedCalls: calls.Observed,
		Unavailable: calls.Unavailable, WorseFeedback: worse, RoutedBp: ratioBp(calls.Routed, calls.Calls),
		SavedUsd: contract.MicroUSD(calls.SavedMicro), WouldSaveUsd: contract.MicroUSD(calls.WouldMicro),
		Guardrail: guardrail(calls, sess), ByPair: r.byPair(pairs),
	}
	return out, nil
}

// guardrail compares the routed (cheap) calls with every other answered (strong) call. Warn is set when the cheap fire
// rate is more than 200 bp above the strong one: the router then costs quality, and the UI says so.
func guardrail(c sqlc.RouterCallStatsRow, s sqlc.RouterSessionStatsRow) contract.RouterGuardrail {
	g := contract.RouterGuardrail{
		CheapCalls: c.CheapCalls, StrongCalls: c.StrongCalls,
		CheapFireBp: ratioBp(c.CheapFired, c.CheapCalls), StrongFireBp: ratioBp(c.StrongFired, c.StrongCalls),
		CheapCompletedBp: ratioBp(s.CheapCompleted, s.CheapClosed), StrongCompletedBp: ratioBp(s.StrongCompleted, s.StrongClosed),
	}
	g.Warn = g.CheapFireBp != nil && g.StrongFireBp != nil && *g.CheapFireBp-*g.StrongFireBp > WarnMarginBp
	return g
}

// WarnMarginBp is how far the cheap fire rate may exceed the strong one before the guardrail warns (200 bp = 2 points).
const WarnMarginBp = 200

// ratioBp is part/whole in basis points, rounded half up, nil without a whole.
func ratioBp(part, whole int64) *int {
	if whole <= 0 {
		return nil
	}
	return new(int((part*10000 + whole/2) / whole))
}

// byPair folds the per-requested-model rows into the default pairs: a dated id counts for its base pair, so
// gpt-4.1 and gpt-4.1-2025-04-14 are one row. Rows keep the saving order of the query.
func (r *RouterReader) byPair(rows []sqlc.RouterPairStatsRow) []contract.RouterPairStat {
	out := []contract.RouterPairStat{}
	for _, row := range rows {
		from := row.FromModel
		for _, p := range r.defaults {
			if p.Provider == row.Provider && p.To == row.ToModel && contract.ModelMatches(p.From, row.FromModel) {
				from = p.From
				break
			}
		}
		i := slices.IndexFunc(out, func(o contract.RouterPairStat) bool {
			return o.Provider == row.Provider && o.From == from && o.To == row.ToModel
		})
		if i < 0 {
			out = append(out, contract.RouterPairStat{Provider: row.Provider, From: from, To: row.ToModel})
			i = len(out) - 1
		}
		out[i].Calls += row.Calls
		out[i].SavedUsd += contract.MicroUSD(row.SavedMicro)
	}
	slices.SortStableFunc(out, func(a, b contract.RouterPairStat) int { return int(b.SavedUsd - a.SavedUsd) })
	return out
}

// SetFeedback sets (worse or fine) or clears (nil) the verdict on a call of the workspace. A call outside the
// workspace, or a malformed id, is not_found; the audit row is written by the API layer.
func (r *RouterReader) SetFeedback(ctx context.Context, workspaceID, callID, userID string, verdict *string) (contract.RouterFeedbackView, error) {
	if !validID(workspaceID) || callID == "" || len(callID) > 200 {
		return contract.RouterFeedbackView{}, errNotFound
	}
	if !contract.ValidRouterVerdict(verdict) {
		return contract.RouterFeedbackView{}, contract.Invalid("verdict", "must be \"worse\", \"fine\" or null")
	}
	found, err := r.s.q.RouterCallExists(ctx, sqlc.RouterCallExistsParams{WorkspaceID: workspaceID, CallID: callID})
	if err != nil {
		return contract.RouterFeedbackView{}, queryErr(err)
	}
	if !found {
		return contract.RouterFeedbackView{}, errNotFound
	}
	view := contract.RouterFeedbackView{CallID: callID}
	if verdict == nil {
		return view, queryErr(r.s.q.RouterFeedbackDelete(ctx, sqlc.RouterFeedbackDeleteParams{WorkspaceID: workspaceID, CallID: callID}))
	}
	user := pgtype.Text{}
	if validID(userID) {
		user = pgtype.Text{String: userID, Valid: true}
	}
	got, err := r.s.q.RouterFeedbackUpsert(ctx, sqlc.RouterFeedbackUpsertParams{WorkspaceID: workspaceID, CallID: callID, Verdict: *verdict, UserID: user})
	if err != nil {
		return contract.RouterFeedbackView{}, queryErr(err)
	}
	view.Verdict = &got
	return view, nil
}
