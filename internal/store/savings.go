package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// window is the [from, to) timestamp pair of an inclusive UTC-day range.
func window(r contract.SavingsRange) (from, to time.Time) { return r.From, r.To.AddDate(0, 0, 1) }

// readSnapshot runs fn on one REPEATABLE READ, read-only snapshot, so the several reads of a savings
// payload see the same rows: the hero, the days and the causes always agree, even while calls land.
func (s *Store) readSnapshot(ctx context.Context, fn func(q *sqlc.Queries) error) error {
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		return fn(s.q.WithTx(tx))
	})
	return queryErr(err)
}

// Overview reads the savings dashboard of a workspace for a validated range (ruling S2: money from
// monitor_events by ts, spend from calls by started_at, UTC day buckets). The days array covers the whole
// range, zero-filled. ByCause holds the six monitors in MonitorNames order with Mode, Confidence left for
// the caller to fill from the workspace's monitor settings, and Upsell is left empty for the same reason.
// The three savings classes are separate fields everywhere and never summed (ADR 0023).
func (s *Store) Overview(ctx context.Context, workspaceID string, r contract.SavingsRange) (contract.SavingsOverview, error) {
	if !validID(workspaceID) {
		return contract.SavingsOverview{}, contract.Invalid("workspaceId", "must be a workspace id")
	}
	from, to := window(r)
	var (
		spend    []sqlc.SavingsSpendByDayRow
		events   []sqlc.SavingsEventsByDayRow
		stats    []contract.MonitorStatRow
		tops     []sqlc.SavingsCauseTopsRow
		first    sqlc.CallStatusRow
		acted    bool
		spendArg = sqlc.SavingsSpendByDayParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)}
		evArg    = sqlc.SavingsEventsByDayParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)}
		topArg   = sqlc.SavingsCauseTopsParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)}
	)
	err := s.readSnapshot(ctx, func(q *sqlc.Queries) (err error) {
		if spend, err = q.SavingsSpendByDay(ctx, spendArg); err != nil {
			return err
		}
		if events, err = q.SavingsEventsByDay(ctx, evArg); err != nil {
			return err
		}
		if stats, err = s.monitorStatsWith(ctx, q, workspaceID, from, to); err != nil {
			return err
		}
		if tops, err = q.SavingsCauseTops(ctx, topArg); err != nil {
			return err
		}
		if first, err = q.CallStatus(ctx, sqlc.CallStatusParams{WorkspaceID: workspaceID, SinceTs: pgts(to)}); err != nil {
			return err
		}
		acted, err = q.SavingsHasActed(ctx, workspaceID)
		return err
	})
	if err != nil {
		return contract.SavingsOverview{}, err
	}
	out := contract.SavingsOverview{
		Range: r.Range, From: r.FromDay(), To: r.ToDay(), Timezone: "UTC", State: stateOf(first.FirstCallAt.Valid, acted),
		Upsell: []contract.UpsellView{},
	}
	if t := tsp(first.FirstCallAt); t != nil {
		out.FirstCallAt = new(contract.RFC3339(*t))
	}
	out.Days, out.Hero = daysAndHero(r, spend, events)
	out.ByCause = causesOf(stats, tops)
	return out, nil
}

func stateOf(hasCalls, acted bool) string {
	switch {
	case !hasCalls:
		return contract.SavingsNoCalls
	case !acted:
		return contract.SavingsCollecting
	}
	return contract.SavingsActive
}

// daysAndHero zero-fills the range's days from the two per-day reads and takes the hero from their totals
// rows. WouldHavePaid is paid + saved (certain only) per day and in the hero.
func daysAndHero(r contract.SavingsRange, spend []sqlc.SavingsSpendByDayRow, events []sqlc.SavingsEventsByDayRow) ([]contract.SavingsDay, contract.SavingsHero) {
	byDay := make(map[string]*contract.SavingsDay, r.Days())
	days := make([]contract.SavingsDay, r.Days())
	for i := range days {
		days[i].Day = r.From.AddDate(0, 0, i).Format(time.DateOnly)
		byDay[days[i].Day] = &days[i]
	}
	var hero contract.SavingsHero
	for _, e := range spend {
		if !e.Day.Valid {
			hero.PaidUsd, hero.UnpricedCalls = contract.MicroUSD(e.PaidMicro), e.Unpriced
			continue
		}
		if d := byDay[e.Day.Time.UTC().Format(time.DateOnly)]; d != nil {
			d.PaidUsd, d.Calls, d.Sessions = contract.MicroUSD(e.PaidMicro), e.Calls, e.Sessions
		}
	}
	for _, e := range events {
		if !e.Day.Valid {
			hero.SavedUsd, hero.LikelySavedUsd, hero.WouldSaveUsd = contract.MicroUSD(e.SavedMicro), contract.MicroUSD(e.LikelyMicro), contract.MicroUSD(e.WouldSaveMicro)
			hero.SavedTokens, hero.LikelySavedTokens, hero.WouldSaveTokens = e.SavedTokens, e.LikelySavedTokens, e.WouldSaveTokens
			continue
		}
		if d := byDay[e.Day.Time.UTC().Format(time.DateOnly)]; d != nil {
			d.SavedUsd, d.LikelySavedUsd = contract.MicroUSD(e.SavedMicro), contract.MicroUSD(e.LikelyMicro)
		}
	}
	for i := range days {
		days[i].WouldHavePaidUsd = days[i].PaidUsd + days[i].SavedUsd
	}
	hero.WouldHavePaidUsd = hero.PaidUsd + hero.SavedUsd
	hero.SavedBp = basisPoints(hero.SavedUsd, hero.PaidUsd+hero.SavedUsd)
	return days, hero
}

// basisPoints is part/whole in integer basis points (0-10000, rounded half up), nil when whole is 0.
func basisPoints(part, whole contract.MicroUSD) *int {
	if whole <= 0 {
		return nil
	}
	bp := int((int64(max(part, 0))*10000 + int64(whole)/2) / int64(whole))
	return &bp
}

// causesOf builds the six CauseView rows in MonitorNames order from the monitor stats and the per-monitor tops.
func causesOf(stats []contract.MonitorStatRow, tops []sqlc.SavingsCauseTopsRow) []contract.CauseView {
	type top struct {
		client, tool, path, hash string
		repeats, toolN           int64
	}
	by := map[contract.MonitorName]*top{}
	for _, t := range tops {
		m := contract.MonitorName(t.Monitor)
		if by[m] == nil {
			by[m] = &top{}
		}
		switch t.Dim {
		case "client":
			by[m].client = t.Val
		case "tool":
			by[m].tool, by[m].toolN = t.Val, t.N
		case "path":
			by[m].path, by[m].hash, by[m].repeats = t.Val, t.Val2, t.N
		}
	}
	out := make([]contract.CauseView, len(stats))
	for i, st := range stats {
		c := contract.CauseView{
			Monitor: st.Monitor, Fires: st.Fires, SavedUsd: st.Saved, LikelySavedUsd: st.Likely, WouldSaveUsd: st.WouldSave,
			SavedTokens: st.SavedTokens, LikelySavedTokens: st.LikelySavedTokens, WouldSaveTokens: st.WouldSaveTokens,
		}
		if t := by[st.Monitor]; t != nil {
			c.TopClient, c.TopTool = textPtr(t.client), textPtr(t.tool)
			c.TopRepeatCount = max(t.repeats, t.toolN)
			if t.path != "" {
				c.TopPath = &t.path
			} else if t.hash != "" {
				c.TopPathHash = new(hashPrefix(t.hash))
			}
		}
		out[i] = c
	}
	return out
}

func textPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// hashPrefix is the first 8 characters of a stored path hash.
func hashPrefix(h string) string { return h[:min(len(h), 8)] }
