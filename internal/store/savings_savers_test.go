package store_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// seedSavers adds to seedSavings' monitor rows the trim and router calls of one session s3 (03-09), a hint session s4
// and nothing else: acted certain trim 2.0 (1000 tokens), acted likely trim 0.5 (500), observe trim 0.3 (300),
// routed 0.4, observe route 0.1.
func seedSavers(t *testing.T, s *store.Store, ws string) {
	t.Helper()
	at := func(i int) time.Time { return day(9).Add(time.Duration(i) * time.Hour) }
	hint := rec(ws, "c20", "s4", 1, at(5), "ok", true, mu(1_000_000))
	hint.HintInjected = true
	batch := []contract.CallRecord{
		trimRec(rec(ws, "c10", "s3", 1, at(1), "ok", false, mu(3_000_000)), contract.TrimActed, contract.ConfidenceCertain, 1000, 2_000_000, contract.StageDupLines),
		trimRec(rec(ws, "c11", "s3", 2, at(2), "ok", false, mu(3_000_000)), contract.TrimActed, contract.ConfidenceLikely, 500, 500_000, contract.StageLogWindow),
		trimRec(rec(ws, "c12", "s3", 3, at(3), "ok", false, mu(3_000_000)), contract.TrimObserve, contract.ConfidenceLikely, 300, 300_000, contract.StageJSONTable),
		routeRec(rec(ws, "c13", "s3", 4, at(4), "ok", false, mu(1_000_000)), contract.RouteRouted, 400_000),
		routeRec(rec(ws, "c14", "s3", 5, at(5), "ok", false, mu(1_000_000)), contract.RouteObserve, 100_000),
		hint,
	}
	if _, err := s.WriteCalls(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
}

func TestOverviewAddsTrimAndRouterToTheHeadlineAndKeepsEveryClassApart(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws, other := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA"), newWS(t, s, "B", "BBBBBBBBBBBBBBBBBBBBBB")
	for _, w := range []string{ws, other} { // another workspace's identical rows must never be counted
		seedSavings(t, s, w)
		seedSavers(t, s, w)
	}
	o, err := s.Overview(ctx, ws, rangeOf(8, 10))
	if err != nil {
		t.Fatal(err)
	}
	h := o.Hero
	// saved = monitor certain 1.0 + trim certain 2.0 + routed 0.4; likely = monitor 0.6 + trim 0.5; would = monitor 0.2 + trim 0.3 + route 0.1
	if h.SavedUsd != 3_400_000 || h.LikelySavedUsd != 1_100_000 || h.WouldSaveUsd != 600_000 ||
		h.SavedTokens != 1100 || h.LikelySavedTokens != 560 || h.WouldSaveTokens != 320 {
		t.Fatalf("hero = %+v", h)
	}
	var sessSaved, sessLikely contract.MicroUSD // R6: the headline is SUM(sessions.saved_usd) of the sessions in range
	for _, id := range []string{"s1", "s2", "s3", "s4"} {
		sess, _, _, err := s.Session(ctx, ws, id)
		if err != nil {
			t.Fatal(err)
		}
		sessSaved, sessLikely = sessSaved+sess.Saved, sessLikely+sess.LikelySaved
	}
	if sessSaved != h.SavedUsd || sessLikely != h.LikelySavedUsd {
		t.Errorf("sessions %v/%v != hero %v/%v", sessSaved, sessLikely, h.SavedUsd, h.LikelySavedUsd)
	}
	checkCauses(t, o.ByCause, h)
	trim, router, memory := o.ByCause[6], o.ByCause[7], o.ByCause[8]
	if trim.Fires != 3 || trim.SavedUsd != 2_000_000 || trim.LikelySavedUsd != 500_000 || trim.WouldSaveUsd != 300_000 ||
		trim.SavedTokens != 1000 || trim.LikelySavedTokens != 500 || trim.WouldSaveTokens != 300 {
		t.Errorf("trim = %+v", trim)
	}
	if router.Fires != 2 || router.SavedUsd != 400_000 || router.WouldSaveUsd != 100_000 || router.LikelySavedUsd != 0 || router.SavedTokens != 0 {
		t.Errorf("router = %+v", router)
	}
	if memory.Kind != contract.KindMemory || memory.Fires != 0 || memory.SavedUsd != 0 || memory.LikelySavedUsd != 0 || memory.WouldSaveUsd != 0 {
		t.Errorf("memory is a zero row until it records hits: %+v", memory)
	}
	// the days add up to the hero for saved and likely
	var dSaved, dLikely contract.MicroUSD
	for _, d := range o.Days {
		dSaved, dLikely = dSaved+d.SavedUsd, dLikely+d.LikelySavedUsd
		if d.WouldHavePaidUsd != d.PaidUsd+d.SavedUsd {
			t.Errorf("%s: wouldHavePaid %v != paid + saved", d.Day, d.WouldHavePaidUsd)
		}
	}
	if dSaved != h.SavedUsd || dLikely != h.LikelySavedUsd {
		t.Errorf("days %v/%v != hero %v/%v", dSaved, dLikely, h.SavedUsd, h.LikelySavedUsd)
	}
	noFieldSumsClasses(t, o)

	// the per-client table adds the same saved and likely money
	causes, _, err := s.Causes(ctx, ws, rangeOf(8, 10))
	if err != nil {
		t.Fatal(err)
	}
	var cSaved, cLikely contract.MicroUSD
	for _, c := range causes.Clients {
		cSaved, cLikely = cSaved+c.SavedUsd, cLikely+c.LikelySavedUsd
	}
	if cSaved != h.SavedUsd || cLikely != h.LikelySavedUsd {
		t.Errorf("clients %v/%v != hero %v/%v", cSaved, cLikely, h.SavedUsd, h.LikelySavedUsd)
	}
	if causes.Causes[6].Cause != contract.CauseTrim || causes.Causes[6].SavedUsd != 2_000_000 {
		t.Errorf("the Causes read carries the trim card: %+v", causes.Causes[6])
	}
	if st, err := s.Stats(ctx, ws, day(8), day(11)); err != nil || st.Saved != h.SavedUsd || st.Likely != h.LikelySavedUsd {
		t.Errorf("stats saved/likely = %v/%v, %v", st.Saved, st.Likely, err)
	}
}

// noFieldSumsClasses walks the payload as JSON: in no object does a money field equal saved + likely.
func noFieldSumsClasses(t *testing.T, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatal(err)
	}
	var walk func(path string, n any)
	walk = func(path string, n any) {
		switch x := n.(type) {
		case []any:
			for _, e := range x {
				walk(path+"[]", e)
			}
		case map[string]any:
			sv, lv := x["savedUsd"], x["likelySavedUsd"]
			if s, ok := sv.(string); ok {
				if l, ok := lv.(string); ok && s != "0.000000" && l != "0.000000" {
					a, _ := contract.ParseMicroUSD(s)
					b, _ := contract.ParseMicroUSD(l)
					sum := a + b
					for k, f := range x {
						if str, ok := f.(string); ok && strings.HasSuffix(k, "Usd") && k != "savedUsd" && k != "likelySavedUsd" {
							if c, err := contract.ParseMicroUSD(str); err == nil && c == sum {
								t.Errorf("%s.%s = %s equals saved + likely", path, k, str)
							}
						}
					}
				}
			}
			for k, e := range x {
				walk(path+"."+k, e)
			}
		}
	}
	walk("$", tree)
}

func TestObserveMovesOnlyWouldSave(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	at := day(9).Add(time.Hour)
	batch := []contract.CallRecord{
		trimRec(rec(ws, "o1", "so", 1, at, "ok", true, mu(3_000_000)), contract.TrimObserve, contract.ConfidenceLikely, 800, 900_000, contract.StageDupLines),
		routeRec(rec(ws, "o2", "so", 2, at.Add(time.Minute), "ok", true, mu(1_000_000)), contract.RouteObserve, 250_000),
	}
	if _, err := s.WriteCalls(ctx, batch); err != nil {
		t.Fatal(err)
	}
	o, err := s.Overview(ctx, ws, rangeOf(8, 10))
	if err != nil {
		t.Fatal(err)
	}
	h := o.Hero
	if h.SavedUsd != 0 || h.LikelySavedUsd != 0 || h.SavedTokens != 0 || h.LikelySavedTokens != 0 || h.WouldSaveUsd != 1_150_000 || h.WouldSaveTokens != 800 {
		t.Fatalf("observe must move only would-save: %+v", h)
	}
	if o.State != contract.SavingsCollecting {
		t.Errorf("state = %s: nothing has acted yet", o.State)
	}
	sess, _, _, err := s.Session(ctx, ws, "so")
	if err != nil || sess.Saved != 0 || sess.LikelySaved != 0 || sess.TrimTokens != 0 || sess.RoutedCalls != 0 {
		t.Errorf("an observed session carries no saved money: %+v %v", sess, err)
	}
}

func TestListSessionsTagFilter(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	seedSavings(t, s, ws)
	seedSavers(t, s, ws)
	for tag, want := range map[string][]string{
		contract.SessionTagTrimmed:    {"s3"},
		contract.SessionTagRouted:     {"s3"},
		contract.SessionTagRemembered: {"s4"},
	} {
		page, err := s.ListSessions(ctx, contract.SessionQuery{WorkspaceID: ws, Tag: tag})
		if err != nil || len(page.Items) != len(want) || page.Items[0].ID != want[0] {
			t.Errorf("tag %s = %+v, %v; want %v", tag, page.Items, err, want)
		}
	}
	page, _ := s.ListSessions(ctx, contract.SessionQuery{WorkspaceID: ws, Tag: contract.SessionTagTrimmed})
	if got := page.Items[0]; got.TrimTokens != 1500 || got.RoutedCalls != 1 || got.Hints != 0 {
		t.Errorf("session list row = %+v", got)
	}
	if _, err := s.ListSessions(ctx, contract.SessionQuery{WorkspaceID: ws, Tag: "weird"}); code(err) != contract.CodeValidationFailed {
		t.Errorf("unknown tag = %v", err)
	}
	_, calls, _, err := s.Session(ctx, ws, "s3")
	if err != nil || len(calls) != 5 {
		t.Fatalf("calls = %d, %v", len(calls), err)
	}
	c := calls[0]
	if c.TrimAction == nil || *c.TrimAction != contract.TrimActed || c.TrimConfidence == nil || *c.TrimConfidence != contract.ConfidenceCertain ||
		c.TrimTokens != 1000 || c.SavedUSDTrim != 2_000_000 || c.RouterAction != nil {
		t.Errorf("trim call read back = %+v", c)
	}
	if r := calls[3]; r.RouterAction == nil || *r.RouterAction != contract.RouteRouted || r.RoutedFrom != "gpt-4o" || r.RoutedTo != "gpt-4o-mini" ||
		r.RouterScoreBp == nil || *r.RouterScoreBp != 1200 || r.SavedUSDRouter != 400_000 {
		t.Errorf("routed call read back = %+v", r)
	}
	if untouched := calls[0].HintInjected; untouched {
		t.Error("hint flag on the wrong call")
	}
}
