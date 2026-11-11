package store

import (
	"context"
	"math/big"
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
// range, zero-filled. ByCause holds nine rows with Mode, Confidence left for
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
		callSav  []sqlc.SavingsCallSaversRow
		first    sqlc.CallStatusRow
		acted    bool
		spendArg = sqlc.SavingsSpendByDayParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)}
		evArg    = sqlc.SavingsEventsByDayParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)}
		savArg   = sqlc.SavingsCallSaversParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)}
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
		if callSav, err = q.SavingsCallSavers(ctx, savArg); err != nil {
			return err
		}
		if first, err = q.CallStatus(ctx, sqlc.CallStatusParams{WorkspaceID: workspaceID, SinceTs: pgts(to)}); err != nil {
			return err
		}
		if acted = actedMoney(events, callSav); !acted { // acted money in the range already proves a monitor acted
			acted, err = q.SavingsHasActed(ctx, workspaceID)
		}
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
	out.Days, out.Hero = daysAndHero(r, spend, events, callSav)
	for _, card := range causesOf(stats, nil, callSav) {
		out.ByCause = append(out.ByCause, card.CauseView)
	}
	return out, nil
}

// actedMoney is true when the range's totals row holds saved or likely money, which only acted events produce.
func actedMoney(events []sqlc.SavingsEventsByDayRow, callSav []sqlc.SavingsCallSaversRow) bool {
	for _, e := range events {
		if !e.Day.Valid && (e.SavedMicro != 0 || e.LikelyMicro != 0) {
			return true
		}
	}
	for _, c := range callSav {
		if !c.Day.Valid && (c.TrimSavedMicro != 0 || c.TrimLikelyMicro != 0 || c.RouterSavedMicro != 0) {
			return true
		}
	}
	return false
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
// rows. The trim and router branches from calls add to the same classes: certain trim and routed money to saved,
// likely trim to likely, observe amounts to would-save. WouldHavePaid is paid + saved (certain only) per day and
// in the hero.
func daysAndHero(r contract.SavingsRange, spend []sqlc.SavingsSpendByDayRow, events []sqlc.SavingsEventsByDayRow, callSav []sqlc.SavingsCallSaversRow) ([]contract.SavingsDay, contract.SavingsHero) {
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
	for _, c := range callSav {
		saved, likely := contract.MicroUSD(c.TrimSavedMicro+c.RouterSavedMicro), contract.MicroUSD(c.TrimLikelyMicro)
		if !c.Day.Valid {
			hero.SavedUsd += saved
			hero.LikelySavedUsd += likely
			hero.WouldSaveUsd += contract.MicroUSD(c.TrimWouldMicro + c.RouterWouldMicro)
			hero.SavedTokens += c.TrimSavedTokens
			hero.LikelySavedTokens += c.TrimLikelyTokens
			hero.WouldSaveTokens += c.TrimWouldTokens
			continue
		}
		if d := byDay[c.Day.Time.UTC().Format(time.DateOnly)]; d != nil {
			d.SavedUsd += saved
			d.LikelySavedUsd += likely
		}
	}
	for i := range days {
		days[i].WouldHavePaidUsd = days[i].PaidUsd + days[i].SavedUsd
	}
	hero.WouldHavePaidUsd = hero.PaidUsd + hero.SavedUsd
	hero.SavedBp = basisPoints(hero.SavedUsd, hero.PaidUsd+hero.SavedUsd)
	return days, hero
}

// basisPoints is part/whole in integer basis points (0-10000, rounded half up), nil when whole is 0. part is
// clamped to [0, whole]. The product part*10000 is formed in big integers so no amount can overflow.
func basisPoints(part, whole contract.MicroUSD) *int {
	if whole <= 0 {
		return nil
	}
	n := new(big.Int).Mul(big.NewInt(int64(min(max(part, 0), whole))), big.NewInt(10000))
	n.Add(n, big.NewInt(int64(whole)/2))
	n.Quo(n, big.NewInt(int64(whole)))
	bp := int(n.Int64()) // 0..10000 by the clamp above
	return &bp
}

// causesOf builds the nine cause rows in AllSavingsCauses order: the six monitors from their stats (with the
// per-monitor tops when the caller read them), then trim and router from the calls branch, then memory. Memory's
// money comes from memory_hits, which lands with the memory feature; until then its row is zero.
func causesOf(stats []contract.MonitorStatRow, tops []sqlc.SavingsCauseTopsRow, callSav []sqlc.SavingsCallSaversRow) []contract.CauseCard {
	return append(monitorCards(stats, tops), saverCards(callSav)...)
}

// monitorCards is one card per monitor stats row, with the per-monitor tops when the caller read them.
func monitorCards(stats []contract.MonitorStatRow, tops []sqlc.SavingsCauseTopsRow) []contract.CauseCard {
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
	out := make([]contract.CauseCard, 0, len(stats))
	for _, st := range stats {
		c := contract.CauseCard{CauseView: contract.CauseView{
			Cause: contract.SavingsCause(st.Monitor), Kind: contract.KindMonitor, Fires: st.Fires, SavedUsd: st.Saved, LikelySavedUsd: st.Likely,
			WouldSaveUsd: st.WouldSave, SavedTokens: st.SavedTokens, LikelySavedTokens: st.LikelySavedTokens, WouldSaveTokens: st.WouldSaveTokens,
		}}
		if t := by[st.Monitor]; t != nil {
			c.TopClient, c.TopTool = textPtr(t.client), textPtr(t.tool)
			c.TopRepeatCount = max(t.repeats, t.toolN)
			if t.path != "" {
				c.TopPath = &t.path
			} else if t.hash != "" {
				c.TopPathHash = new(hashPrefix(t.hash))
			}
		}
		out = append(out, c)
	}
	return out
}

// saverCards is the trim, router and memory cards from the calls branch's totals row.
func saverCards(callSav []sqlc.SavingsCallSaversRow) []contract.CauseCard {
	var tot sqlc.SavingsCallSaversRow
	for _, c := range callSav {
		if !c.Day.Valid {
			tot = c
		}
	}
	return []contract.CauseCard{
		{CauseView: contract.CauseView{
			Cause: contract.CauseTrim, Kind: contract.KindTrim, Fires: tot.TrimFires, SavedUsd: contract.MicroUSD(tot.TrimSavedMicro),
			LikelySavedUsd: contract.MicroUSD(tot.TrimLikelyMicro), WouldSaveUsd: contract.MicroUSD(tot.TrimWouldMicro),
			SavedTokens: tot.TrimSavedTokens, LikelySavedTokens: tot.TrimLikelyTokens, WouldSaveTokens: tot.TrimWouldTokens,
		}},
		{CauseView: contract.CauseView{
			Cause: contract.CauseRouter, Kind: contract.KindRouter, Fires: tot.RouterFires, SavedUsd: contract.MicroUSD(tot.RouterSavedMicro),
			WouldSaveUsd: contract.MicroUSD(tot.RouterWouldMicro),
		}},
		{CauseView: contract.CauseView{Cause: contract.CauseMemory, Kind: contract.KindMemory}},
	}
}

func textPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// hashPrefix is the first 8 characters of a stored path hash.
func hashPrefix(h string) string { return h[:min(len(h), 8)] }
