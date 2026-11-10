package store_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

func day(d int) time.Time { return time.Date(2026, 3, d, 0, 0, 0, 0, time.UTC) }

// rangeOf is the inclusive UTC-day range [from, to] of March 2026.
func rangeOf(from, to int) contract.SavingsRange {
	return contract.SavingsRange{Range: "custom", From: day(from), To: day(to)}
}

func pv(m contract.MonitorName, action contract.MonitorMode, conf contract.Confidence, tokens int64, usd contract.MicroUSD, ev contract.MonitorEvidence) contract.PricedVerdict {
	v := verdict(m, action, conf, tokens, usd)
	v.Evidence = ev
	return v
}

// seedSavings writes the fixture every savings read test uses. In range 03-08..03-10: s1 (client "opencode")
// with a certain block (03-09 23:59), a likely note (03-10 00:01, crossing midnight) and a second likely note on
// the same file; s2 (no client) with an unpriced call and an observe event. Out of range: s9 on 03-05 (client
// "opencode"), whose certain 9.00 save must never show up in a 03-08..03-10 read.
func seedSavings(t *testing.T, s *store.Store, ws string) {
	t.Helper()
	read := func(path string) contract.MonitorEvidence {
		return contract.MonitorEvidence{Tool: "read", Path: path, PathHash: "abcdef0123456789", Count: 3}
	}
	c1 := rec(ws, "c1", "s1", 1, day(9).Add(23*time.Hour+59*time.Minute), "ok", true, mu(5_000_000))
	c1.Blocked = true
	c1.Verdicts = []contract.PricedVerdict{pv(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ConfidenceCertain, 100, 1_000_000, read("src/a.go"))}
	c2 := rec(ws, "c2", "s1", 2, day(10).Add(time.Minute), "ok", true, mu(2_000_000))
	c2.Verdicts = []contract.PricedVerdict{pv(contract.MonitorRepeatedRead, contract.ModeNote, contract.ConfidenceLikely, 50, 500_000, read("src/b.go"))}
	c5 := rec(ws, "c5", "s1", 3, day(10).Add(time.Hour), "ok", true, mu(1_000_000))
	c5.Verdicts = []contract.PricedVerdict{pv(contract.MonitorRepeatedRead, contract.ModeNote, contract.ConfidenceLikely, 10, 100_000, read("src/b.go"))}
	c3 := rec(ws, "c3", "s2", 1, day(10).Add(12*time.Hour), "ok", true, nil) // unpriced
	c3.Client = ""
	c3.Verdicts = []contract.PricedVerdict{pv(contract.MonitorNoProgress, contract.ModeObserve, contract.ConfidenceLikely, 20, 200_000, contract.MonitorEvidence{})}
	c4 := rec(ws, "c4", "s9", 1, day(5).Add(time.Hour), "ok", true, mu(7_000_000))
	c4.Verdicts = []contract.PricedVerdict{pv(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ConfidenceCertain, 900, 9_000_000, read("src/z.go"))}
	if _, err := s.WriteCalls(context.Background(), []contract.CallRecord{c1, c2, c5, c3, c4}); err != nil {
		t.Fatal(err)
	}
}

func TestOverviewHeroDaysAndCauses(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws, other := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA"), newWS(t, s, "B", "BBBBBBBBBBBBBBBBBBBBBB")
	seedSavings(t, s, ws)
	seedSavings(t, s, other) // another workspace's identical rows must never be counted
	o, err := s.Overview(ctx, ws, rangeOf(8, 10))
	if err != nil {
		t.Fatal(err)
	}
	h := o.Hero
	if h.SavedUsd != 1_000_000 || h.LikelySavedUsd != 600_000 || h.WouldSaveUsd != 200_000 || h.PaidUsd != 8_000_000 || h.WouldHavePaidUsd != 9_000_000 ||
		h.SavedTokens != 100 || h.LikelySavedTokens != 60 || h.WouldSaveTokens != 20 || h.UnpricedCalls != 1 || h.SavedBp == nil || *h.SavedBp != 1111 {
		t.Fatalf("hero = %+v (bp %v)", h, h.SavedBp)
	}
	var sessSaved contract.MicroUSD // hero = SUM(sessions.saved_usd) for the sessions inside the range (R6)
	for _, id := range []string{"s1", "s2"} {
		sess, _, _, err := s.Session(ctx, ws, id)
		if err != nil {
			t.Fatal(err)
		}
		sessSaved += sess.Saved
	}
	if sessSaved != h.SavedUsd {
		t.Errorf("SUM(sessions.saved) = %v, hero.saved = %v", sessSaved, h.SavedUsd)
	}
	if o.Range != "custom" || o.From != "2026-03-08" || o.To != "2026-03-10" || o.Timezone != "UTC" || o.State != contract.SavingsActive || o.FirstCallAt == nil {
		t.Fatalf("overview header = %+v", o)
	}
	if len(o.Days) != 3 {
		t.Fatalf("days = %+v", o.Days)
	}
	d8, d9, d10 := o.Days[0], o.Days[1], o.Days[2]
	if d8 != (contract.SavingsDay{Day: "2026-03-08"}) {
		t.Errorf("empty day not zero-filled: %+v", d8)
	}
	if d9.PaidUsd != 5_000_000 || d9.SavedUsd != 1_000_000 || d9.WouldHavePaidUsd != 6_000_000 || d9.Calls != 1 || d9.Sessions != 1 {
		t.Errorf("03-09 = %+v", d9)
	}
	if d10.PaidUsd != 3_000_000 || d10.SavedUsd != 0 || d10.LikelySavedUsd != 600_000 || d10.WouldHavePaidUsd != 3_000_000 || d10.Calls != 3 || d10.Sessions != 2 {
		t.Errorf("03-10 = %+v (a cross-midnight session is bucketed by event time)", d10)
	}
	for _, d := range o.Days {
		if d.WouldHavePaidUsd != d.PaidUsd+d.SavedUsd {
			t.Errorf("%s: wouldHavePaid %v != paid %v + saved %v", d.Day, d.WouldHavePaidUsd, d.PaidUsd, d.SavedUsd)
		}
	}
	checkCauses(t, o.ByCause, h)
}

func checkCauses(t *testing.T, by []contract.CauseView, h contract.SavingsHero) {
	t.Helper()
	names := contract.MonitorNames()
	if len(by) != len(names) {
		t.Fatalf("byCause = %d rows, want 6", len(by))
	}
	var saved, likely, would contract.MicroUSD
	for i, c := range by {
		if c.Monitor != names[i] {
			t.Errorf("row %d is %s, want %s", i, c.Monitor, names[i])
		}
		saved, likely, would = saved+c.SavedUsd, likely+c.LikelySavedUsd, would+c.WouldSaveUsd
	}
	if saved != h.SavedUsd || likely != h.LikelySavedUsd || would != h.WouldSaveUsd {
		t.Errorf("sum of causes %v/%v/%v != hero %v/%v/%v (each class on its own)", saved, likely, would, h.SavedUsd, h.LikelySavedUsd, h.WouldSaveUsd)
	}
	dup, rr, np := by[0], by[1], by[2]
	if dup.Fires != 1 || dup.SavedUsd != 1_000_000 || dup.LikelySavedUsd != 0 || dup.WouldSaveUsd != 0 {
		t.Errorf("duplicate = %+v", dup)
	}
	if rr.Fires != 2 || rr.LikelySavedUsd != 600_000 || rr.SavedUsd != 0 || rr.TopPath == nil || *rr.TopPath != "src/b.go" || rr.TopRepeatCount != 2 ||
		rr.TopTool == nil || *rr.TopTool != "read" || rr.TopClient == nil || *rr.TopClient != "opencode" || rr.TopPathHash != nil {
		t.Errorf("repeated_read = %+v", rr)
	}
	if np.Fires != 1 || np.WouldSaveUsd != 200_000 || np.SavedUsd != 0 || np.LikelySavedUsd != 0 || np.TopClient != nil || np.TopPath != nil {
		t.Errorf("no_progress (observe moves only would-save) = %+v", np)
	}
}

func TestOverviewStates(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	empty, collecting := newWS(t, s, "E", "EEEEEEEEEEEEEEEEEEEEEE"), newWS(t, s, "C", "CCCCCCCCCCCCCCCCCCCCCC")
	o, err := s.Overview(ctx, empty, rangeOf(8, 10))
	if err != nil || o.State != contract.SavingsNoCalls || o.FirstCallAt != nil || len(o.Days) != 3 || o.Hero.SavedBp != nil || len(o.ByCause) != 6 {
		t.Fatalf("empty = %+v, %v", o, err)
	}
	for _, d := range o.Days {
		if d.PaidUsd != 0 || d.Calls != 0 {
			t.Errorf("empty day not zero: %+v", d)
		}
	}
	c1 := rec(collecting, "k1", "ks", 1, day(10), "ok", true, mu(1_000_000))
	c1.Verdicts = []contract.PricedVerdict{pv(contract.MonitorDuplicateToolCall, contract.ModeObserve, contract.ConfidenceCertain, 100, 400_000, contract.MonitorEvidence{Tool: "read"})}
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{c1}); err != nil {
		t.Fatal(err)
	}
	o, err = s.Overview(ctx, collecting, rangeOf(8, 10))
	if err != nil || o.State != contract.SavingsCollecting || o.Hero.SavedUsd != 0 || o.Hero.WouldSaveUsd != 400_000 || o.Hero.SavedBp == nil || *o.Hero.SavedBp != 0 {
		t.Fatalf("collecting = %+v, %v", o, err)
	}
	if _, err := s.Overview(ctx, "not-a-uuid", rangeOf(8, 10)); err == nil {
		t.Error("a malformed workspace id must be a validation error")
	}
}

func TestCausesClientsAndRepeats(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	seedSavings(t, s, ws)
	got, targets, err := s.Causes(ctx, ws, rangeOf(8, 10))
	if err != nil {
		t.Fatal(err)
	}
	if got.Range != "custom" || got.Timezone != "UTC" || len(got.Causes) != 6 || len(got.Clients) != 2 {
		t.Fatalf("causes = %+v", got)
	}
	op, un := got.Clients[0], got.Clients[1]
	if op.Client != "opencode" || op.Calls != 3 || op.Sessions != 1 || op.Blocked != 1 || op.SpentUsd != 8_000_000 || op.SavedUsd != 1_000_000 || op.LikelySavedUsd != 600_000 ||
		op.SavedBp == nil || *op.SavedBp != 1111 || op.AvgCallsPerSession != 3 {
		t.Errorf("opencode = %+v", op)
	}
	if op.PrevAvgCallsPerSession == nil || *op.PrevAvgCallsPerSession != 1 {
		t.Errorf("prev window (03-05..03-07) average = %v, want 1", op.PrevAvgCallsPerSession)
	}
	if un.Client != "unknown" || un.Calls != 1 || un.Sessions != 1 || un.SpentUsd != 0 || un.SavedUsd != 0 || un.PrevAvgCallsPerSession != nil {
		t.Errorf("unknown = %+v", un)
	}
	want := []contract.RepeatTarget{
		{Monitor: contract.MonitorRepeatedRead, Tool: "read", Path: "src/b.go", PathHash: "abcdef0123456789", Count: 2},
		{Monitor: contract.MonitorDuplicateToolCall, Tool: "read", Path: "src/a.go", PathHash: "abcdef0123456789", Count: 1},
	}
	if len(targets) != 2 || targets[0] != want[0] || targets[1] != want[1] {
		t.Fatalf("targets = %+v", targets)
	}
}

func TestSavesPagingIsStableAndScoped(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws, other := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA"), newWS(t, s, "B", "BBBBBBBBBBBBBBBBBBBBBB")
	seedSavings(t, s, ws)
	seedSavings(t, s, other)
	p1, err := s.Saves(ctx, ws, rangeOf(8, 10), "", 2)
	if err != nil || len(p1.Items) != 2 || p1.NextCursor == nil {
		t.Fatalf("page 1 = %+v, %v", p1, err)
	}
	a, b := p1.Items[0], p1.Items[1]
	if a.CallID != "c1" || a.EstSavedUsd != 1_000_000 || a.Action != contract.ModeBlock || a.Confidence != contract.ConfidenceCertain || a.SessionID != "s1" ||
		a.Monitor != contract.MonitorDuplicateToolCall || a.Model != "gpt-4o" || a.Client == nil || *a.Client != "opencode" || a.CallIndex != 1 || a.EstSavedTokens != 100 {
		t.Errorf("first save = %+v", a)
	}
	if b.CallID != "c2" || b.Confidence != contract.ConfidenceLikely || b.EstSavedUsd != 500_000 {
		t.Errorf("second save (likely rows are listed, tagged) = %+v", b)
	}
	p2, err := s.Saves(ctx, ws, rangeOf(8, 10), *p1.NextCursor, 2)
	if err != nil || len(p2.Items) != 1 || p2.NextCursor != nil || p2.Items[0].CallID != "c5" {
		t.Fatalf("page 2 = %+v, %v (observe and out-of-range events are not saves)", p2, err)
	}
	seen := map[string]bool{}
	for _, it := range append(p1.Items, p2.Items...) {
		if seen[it.EventID] {
			t.Errorf("event %s listed twice", it.EventID)
		}
		seen[it.EventID] = true
	}
	big, err := s.Saves(ctx, ws, rangeOf(8, 10), "", 100)
	if err != nil || len(big.Items) != 3 || big.NextCursor != nil {
		t.Fatalf("all = %+v, %v", big, err)
	}
	for _, bad := range []string{"%%%", "bm90IGEgY3Vyc29y"} {
		var ve *contract.Error
		if _, err := s.Saves(ctx, ws, rangeOf(8, 10), bad, 2); !errors.As(err, &ve) || ve.Code != contract.CodeValidationFailed {
			t.Errorf("cursor %q: err = %v, want a validation error", bad, err)
		}
	}
	// every save row is the very event the session detail lists, so the evidence is byte-equal
	_, _, _, evs, err := s.SessionDetail(ctx, ws, "s1")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range big.Items {
		found := false
		for _, e := range evs {
			if strconv.FormatInt(e.ID, 10) == it.EventID && e.CallID == it.CallID && e.EstSavedUSD == it.EstSavedUsd {
				found = true
			}
		}
		if !found {
			t.Errorf("save %+v is not in the session's monitor events", it)
		}
	}
}

func TestListSessionsFilters(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	seedSavings(t, s, ws)
	if _, err := s.CloseSessions(ctx, []contract.SessionClose{{WorkspaceID: ws, SessionID: "s2", At: day(10).Add(13 * time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, client, outcome string
		want                  []string
	}{
		{"no filter", "", "", []string{"s2", "s1", "s9"}},
		{"one client family", "opencode", "", []string{"s1", "s9"}},
		{"unknown is the NULL client", "unknown", "", []string{"s2"}},
		{"a client nobody used", "nope", "", nil},
		{"outcome open", "", "open", []string{"s1", "s9"}},
		{"outcome completed", "", "completed", []string{"s2"}},
		{"both filters", "opencode", "completed", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			page, err := s.ListSessions(ctx, contract.SessionQuery{WorkspaceID: ws, Client: c.client, Outcome: c.outcome})
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, it := range page.Items {
				ids = append(ids, it.ID)
			}
			if len(ids) != len(c.want) {
				t.Fatalf("ids = %v, want %v", ids, c.want)
			}
			for i := range ids {
				if ids[i] != c.want[i] {
					t.Fatalf("ids = %v, want %v", ids, c.want)
				}
			}
		})
	}
	if _, err := s.ListSessions(ctx, contract.SessionQuery{WorkspaceID: ws, Outcome: "weird"}); err == nil {
		t.Error("an unknown outcome must be a validation error")
	}
}
