package api_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
)

func savingVerdict(m contract.MonitorName, mode, action contract.MonitorMode, conf contract.Confidence, tokens int64, usd contract.MicroUSD, path string) contract.PricedVerdict {
	return contract.PricedVerdict{
		MonitorVerdict: contract.MonitorVerdict{Monitor: m, Mode: mode, Action: action, Evidence: contract.MonitorEvidence{Tool: "read_file", Path: path, PathHash: "abcdef0123456789", Count: 3}},
		EstTokens:      tokens, EstUSD: usd, Confidence: conf,
	}
}

// seedSavings writes, relative to now: session s1 (opencode) with a certain block of 1.00, a likely note of
// 0.50 and an observe event of 0.10 on one file; session s2 (no client) with an observe event of 0.20.
func (s *stack) seedSavings(t *testing.T, ws string) {
	t.Helper()
	now := time.Now().UTC()
	a1 := callRec(ws, "a1", "s1", 1, now.Add(-3*time.Hour), microp(5_000_000))
	a1.Blocked = true
	a1.Verdicts = []contract.PricedVerdict{savingVerdict(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ModeBlock, contract.ConfidenceCertain, 100, 1_000_000, "src/a.go")}
	a2 := callRec(ws, "a2", "s1", 2, now.Add(-2*time.Hour), microp(2_000_000))
	a2.Verdicts = []contract.PricedVerdict{savingVerdict(contract.MonitorRepeatedRead, contract.ModeNote, contract.ModeNote, contract.ConfidenceLikely, 50, 500_000, "src/a.go")}
	a3 := callRec(ws, "a3", "s1", 3, now.Add(-time.Hour), microp(1_000_000))
	a3.Verdicts = []contract.PricedVerdict{savingVerdict(contract.MonitorRepeatedRead, contract.ModeObserve, contract.ModeObserve, contract.ConfidenceLikely, 10, 100_000, "src/a.go")}
	b1 := callRec(ws, "b1", "s2", 1, now.Add(-30*time.Minute), nil)
	b1.Client = ""
	b1.Verdicts = []contract.PricedVerdict{savingVerdict(contract.MonitorNoProgress, contract.ModeObserve, contract.ModeObserve, contract.ConfidenceLikely, 20, 200_000, "")}
	s.seedCalls(t, a1, a2, a3, b1)
}

var moneyRe = regexp.MustCompile(`^-?\d+\.\d{6}$`)

// moneyStrings walks a decoded JSON value and returns every decimal-money string in it.
func moneyStrings(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		if moneyRe.MatchString(x) {
			*out = append(*out, x)
		}
	case []any:
		for _, e := range x {
			moneyStrings(e, out)
		}
	case map[string]any:
		for _, e := range x {
			moneyStrings(e, out)
		}
	}
}

func TestSavingsPayloads(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	s.seedSavings(t, ws)
	base := "/api/workspaces/" + ws + "/savings/"

	raws := map[string][]byte{}
	for _, v := range []string{"overview", "causes", "saves"} {
		raws[v] = s.must(t, admin.tok, "GET", base+v, "", 200)
		var doc any
		if err := json.Unmarshal(raws[v], &doc); err != nil {
			t.Fatal(err)
		}
		var money []string
		moneyStrings(doc, &money)
		if len(money) == 0 {
			t.Fatalf("%s: no money fields found, the walk is vacuous", v)
		}
		for _, m := range money {
			if m == "1.500000" { // saved 1.00 + likely 0.50 must never be added anywhere
				t.Errorf("%s holds a money field equal to saved + likely (1.500000): %s", v, raws[v])
			}
		}
	}
	ov := decodeAs[contract.SavingsOverview](t, raws["overview"])
	h := ov.Hero
	if h.SavedUsd != 1_000_000 || h.LikelySavedUsd != 500_000 || h.WouldSaveUsd != 300_000 || h.PaidUsd != 8_000_000 || h.WouldHavePaidUsd != 9_000_000 ||
		h.SavedBp == nil || *h.SavedBp != 1111 || h.UnpricedCalls != 1 {
		t.Fatalf("hero = %+v", h)
	}
	if ov.Range != "7d" || ov.Timezone != "UTC" || ov.State != contract.SavingsActive || len(ov.Days) != 7 || ov.FirstCallAt == nil {
		t.Fatalf("overview = %+v", ov)
	}
	checkOverviewInvariants(t, s, admin.tok, ws, ov)
	for _, want := range []string{`"savedUsd":"1.000000"`, `"likelySavedUsd":"0.500000"`, `"wouldSaveUsd":"0.300000"`, `"savedBp":1111`} {
		if !strings.Contains(string(raws["overview"]), want) {
			t.Errorf("overview lacks %s: %s", want, raws["overview"])
		}
	}

	causes := decodeAs[contract.SavingsCauses](t, raws["causes"])
	if len(causes.Causes) != 6 || len(causes.Clients) != 2 || causes.Clients[0].Client != "opencode" || causes.Clients[1].Client != "unknown" {
		t.Fatalf("causes = %+v", causes)
	}
	if len(causes.Suggestions) != 1 || causes.Suggestions[0].Monitor != contract.MonitorRepeatedRead || causes.Suggestions[0].Path == nil || *causes.Suggestions[0].Path != "src/a.go" || causes.Suggestions[0].Count != 2 {
		t.Fatalf("suggestions = %+v", causes.Suggestions)
	}
	checkSavesMatchSession(t, s, admin.tok, ws, decodeAs[contract.SaveList](t, raws["saves"]))
}

func checkOverviewInvariants(t *testing.T, s *stack, tok, ws string, ov contract.SavingsOverview) {
	t.Helper()
	var saved, likely, would contract.MicroUSD
	for _, c := range ov.ByCause {
		saved, likely, would = saved+c.SavedUsd, likely+c.LikelySavedUsd, would+c.WouldSaveUsd
	}
	if saved != ov.Hero.SavedUsd || likely != ov.Hero.LikelySavedUsd || would != ov.Hero.WouldSaveUsd || len(ov.ByCause) != 6 {
		t.Errorf("sum of causes %v/%v/%v != hero %+v", saved, likely, would, ov.Hero)
	}
	for _, d := range ov.Days {
		if d.WouldHavePaidUsd != d.PaidUsd+d.SavedUsd {
			t.Errorf("%s: wouldHavePaid %v != paid %v + saved %v", d.Day, d.WouldHavePaidUsd, d.PaidUsd, d.SavedUsd)
		}
	}
	// the hero is SUM(sessions.saved_usd) for sessions inside the range
	list := decodeAs[contract.SessionList](t, s.must(t, tok, "GET", "/api/workspaces/"+ws+"/sessions", "", 200))
	var fromSessions contract.MicroUSD
	for _, it := range list.Items {
		fromSessions += it.SavedUsd
	}
	if fromSessions != ov.Hero.SavedUsd {
		t.Errorf("SUM(sessions.savedUsd) = %v, hero = %v", fromSessions, ov.Hero.SavedUsd)
	}
	// observe moves only would-save and the upsell, never saved or likely
	if ov.ByCause[2].Monitor != contract.MonitorNoProgress || ov.ByCause[2].SavedUsd != 0 || ov.ByCause[2].LikelySavedUsd != 0 || ov.ByCause[2].WouldSaveUsd != 200_000 || ov.ByCause[2].Mode != contract.ModeObserve {
		t.Errorf("no_progress = %+v", ov.ByCause[2])
	}
	if len(ov.Upsell) != 2 || ov.Upsell[0].Monitor != contract.MonitorNoProgress || ov.Upsell[0].WouldSaveUsd != 200_000 || ov.Upsell[1].Monitor != contract.MonitorRepeatedRead || ov.Upsell[1].WouldSaveUsd != 100_000 {
		t.Errorf("upsell = %+v", ov.Upsell)
	}
	if rr := ov.ByCause[1]; rr.Confidence != contract.ConfidenceLikely || rr.Mode != contract.ModeObserve || ov.ByCause[0].Confidence != contract.ConfidenceLikely || ov.ByCause[3].Confidence != contract.ConfidenceNone {
		t.Errorf("modes and classes = %+v", ov.ByCause)
	}
}

// Each save row is an event of the session it names, with the same id, call and money.
func checkSavesMatchSession(t *testing.T, s *stack, tok, ws string, saves contract.SaveList) {
	t.Helper()
	if len(saves.Items) != 2 || saves.NextCursor != nil || saves.Items[0].EstSavedUsd != 1_000_000 || saves.Items[1].EstSavedUsd != 500_000 {
		t.Fatalf("saves = %+v (the observe events are not saves)", saves)
	}
	d := decodeAs[contract.SessionDetail](t, s.must(t, tok, "GET", "/api/sessions/s1?workspaceId="+ws, "", 200))
	for _, it := range saves.Items {
		found := false
		for _, e := range d.MonitorEvents {
			if e.ID == it.EventID && e.CallID == it.CallID && e.EstSavedUsd == it.EstSavedUsd && e.Evidence.Path == "src/a.go" {
				found = true
			}
		}
		if !found || it.SessionID != "s1" {
			t.Errorf("save %+v is not an event of its session: %+v", it, d.MonitorEvents)
		}
	}
	if len(d.Insight.Repeats) != 1 || d.Insight.Repeats[0].Count != 2 || d.Insight.Suggestion == nil || !strings.Contains(*d.Insight.Suggestion, "src/a.go") {
		t.Errorf("insight = %+v", d.Insight)
	}
}

func TestSavingsEmptyWorkspaceAndPaging(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	empty := decodeAs[contract.SavingsOverview](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/savings/overview?range=30d", "", 200))
	if empty.State != contract.SavingsNoCalls || len(empty.Days) != 30 || empty.FirstCallAt != nil || empty.Hero.SavedBp != nil || len(empty.ByCause) != 6 || empty.Upsell == nil {
		t.Fatalf("empty = %+v", empty)
	}
	raw := string(s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/savings/causes", "", 200))
	for _, want := range []string{`"clients":[]`, `"suggestions":[]`} {
		if !strings.Contains(raw, want) {
			t.Errorf("empty causes lacks %s: %s", want, raw)
		}
	}
	s.seedSavings(t, ws)
	p1 := decodeAs[contract.SaveList](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/savings/saves?limit=1", "", 200))
	if len(p1.Items) != 1 || p1.NextCursor == nil || p1.Items[0].EstSavedUsd != 1_000_000 {
		t.Fatalf("page 1 = %+v", p1)
	}
	p2 := decodeAs[contract.SaveList](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/savings/saves?limit=1&cursor="+*p1.NextCursor, "", 200))
	if len(p2.Items) != 1 || p2.NextCursor != nil || p2.Items[0].EstSavedUsd != 500_000 {
		t.Fatalf("page 2 = %+v", p2)
	}
	// a custom range ending two days ago excludes everything seeded (at most 3 h old). Ending it yesterday made the
	// test fail between 00:00 and 03:00 UTC, when the seeded calls fall on yesterday's UTC date.
	y := time.Now().UTC().AddDate(0, 0, -3).Format(time.DateOnly)
	z := time.Now().UTC().AddDate(0, 0, -2).Format(time.DateOnly)
	far := decodeAs[contract.SavingsOverview](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/savings/overview?range=custom&from="+y+"&to="+z, "", 200))
	if far.Range != "custom" || len(far.Days) != 2 || far.Hero.SavedUsd != 0 || far.Hero.PaidUsd != 0 || far.From != y || far.To != z {
		t.Fatalf("custom range = %+v", far)
	}
}

func TestSavingsRouteTable(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	today := time.Now().UTC()
	day := func(back int) string { return today.AddDate(0, 0, -back).Format(time.DateOnly) }
	cases := []struct {
		name, query string
		status      int
		field       string
	}{
		{"default range", "", 200, ""},
		{"7d", "?range=7d", 200, ""},
		{"30d", "?range=30d", 200, ""},
		{"custom", "?range=custom&from=" + day(5) + "&to=" + day(1), 200, ""},
		{"custom up to today", "?range=custom&from=" + day(0) + "&to=" + day(0), 200, ""},
		{"90 days", "?range=custom&from=" + day(89) + "&to=" + day(0), 200, ""},
		{"unknown range", "?range=1y", 422, "range"},
		{"case-sensitive", "?range=7D", 422, "range"},
		{"from without custom", "?range=7d&from=" + day(1), 422, "from"},
		{"to without custom", "?to=" + day(1), 422, "to"},
		{"custom needs from", "?range=custom&to=" + day(1), 422, "from"},
		{"custom needs to", "?range=custom&from=" + day(1), 422, "to"},
		{"bad from", "?range=custom&from=yesterday&to=" + day(1), 422, "from"},
		{"bad to", "?range=custom&from=" + day(2) + "&to=2026-13-40", 422, "to"},
		{"from after to", "?range=custom&from=" + day(1) + "&to=" + day(3), 422, "from"},
		{"to in the future", "?range=custom&from=" + day(1) + "&to=" + today.AddDate(0, 0, 1).Format(time.DateOnly), 422, "to"},
		{"91 days", "?range=custom&from=" + day(90) + "&to=" + day(0), 422, "from"},
	}
	for _, view := range []string{"overview", "causes", "saves"} {
		for _, c := range cases {
			t.Run(view+"/"+c.name, func(t *testing.T) {
				code, b := s.call(t, "GET", "/api/workspaces/"+ws+"/savings/"+view+c.query, admin.tok, "")
				if code != c.status {
					t.Fatalf("status %d, want %d: %s", code, c.status, b)
				}
				if c.status == 422 {
					wantErr(t, code, b, 422, contract.CodeValidationFailed)
					if !strings.Contains(string(b), `"`+c.field+`:`) {
						t.Errorf("the error does not name %q: %s", c.field, b)
					}
				}
			})
		}
	}
	for _, q := range []string{"?limit=0", "?limit=201", "?limit=abc", "?limit=-1"} {
		code, b := s.call(t, "GET", "/api/workspaces/"+ws+"/savings/saves"+q, admin.tok, "")
		wantErr(t, code, b, 422, contract.CodeValidationFailed)
		if !strings.Contains(string(b), contract.LimitMessage) { // the one message every paged list uses
			t.Errorf("limit %s: message is not contract.LimitMessage: %s", q, b)
		}
	}
	code, b := s.call(t, "GET", "/api/workspaces/"+ws+"/savings/saves?cursor=garbage", admin.tok, "")
	wantErr(t, code, b, 422, contract.CodeValidationFailed)
	s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/savings/saves?limit=200", "", 200) // the shared maximum
	for _, view := range []string{"overview", "causes", "saves"} {
		for _, p := range []string{"00000000-0000-0000-0000-00000000dead", "not-a-uuid"} {
			code, b := s.call(t, "GET", "/api/workspaces/"+p+"/savings/"+view, admin.tok, "")
			wantErr(t, code, b, 404, contract.CodeNotFound)
		}
		code, b := s.call(t, "GET", "/api/workspaces/"+ws+"/savings/"+view, "", "")
		wantErr(t, code, b, 401, contract.CodeUnauthenticated)
	}
}

func TestSavingsPermissionsAndWorkspaceScope(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	_, ws := s.defaultWorkspace(t, admin.tok)
	other := s.newWorkspace(t, admin.tok, "Other")
	s.seedSavings(t, ws)
	put := func(grants ...string) {
		s.must(t, admin.tok, "PUT", "/api/organization/permissions", `{"grants":[`+strings.Join(grants, ",")+`]}`, 200)
	}
	grant := func(rt, act string) string {
		return `{"resourceType":"` + rt + `","action":"` + act + `","workspaceId":null}`
	}
	routes := []string{"overview", "causes", "saves"}
	for _, c := range []struct {
		grants []string
		want   int
	}{
		{[]string{grant("workspace", "read")}, 403},
		{[]string{grant("workspace", "read"), grant("session", "read")}, 403}, // session:read is not savings:read
		{[]string{grant("workspace", "read"), grant("savings", "read")}, 200},
	} {
		put(c.grants...)
		for _, r := range routes {
			if code, b := s.call(t, "GET", "/api/workspaces/"+ws+"/savings/"+r, m.tok, ""); code != c.want {
				t.Fatalf("%s with %v: %d %s, want %d", r, c.grants, code, b, c.want)
			}
		}
	}
	// another workspace of the same org never shows this workspace's rows
	ov := decodeAs[contract.SavingsOverview](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+other+"/savings/overview", "", 200))
	if ov.State != contract.SavingsNoCalls || ov.Hero.SavedUsd != 0 || ov.Hero.PaidUsd != 0 {
		t.Fatalf("other workspace overview = %+v", ov)
	}
}

func TestSessionListClientAndOutcomeFilters(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	s.seedSavings(t, ws)
	for q, want := range map[string]int{"": 2, "?client=opencode": 1, "?client=unknown": 1, "?client=nobody": 0, "?outcome=open": 2, "?outcome=completed": 0, "?client=opencode&outcome=open": 1} {
		list := decodeAs[contract.SessionList](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/sessions"+q, "", 200))
		if len(list.Items) != want {
			t.Errorf("sessions%s = %d items, want %d", q, len(list.Items), want)
		}
	}
	for _, q := range []string{"?outcome=weird", "?client=" + strings.Repeat("x", 65)} {
		code, b := s.call(t, "GET", "/api/workspaces/"+ws+"/sessions"+q, admin.tok, "")
		wantErr(t, code, b, 422, contract.CodeValidationFailed)
	}
	// a session with no repetition has an empty insight, never null
	d := string(s.must(t, admin.tok, "GET", "/api/sessions/s2?workspaceId="+ws, "", 200))
	if !strings.Contains(d, `"insight":{"repeats":[],"suggestion":null}`) {
		t.Errorf("insight of a session without repeats: %s", d)
	}
}

func TestSavingsRoutesAnswerNotAvailableWhenNotWired(t *testing.T) {
	f := newFixture(t, func(d *api.Deps) { d.Savings = nil })
	for _, v := range []string{"overview", "causes", "saves"} {
		resp, b := f.do(t, "GET", "/api/workspaces/w/savings/"+v, "")
		wantErr(t, resp.StatusCode, b, 501, contract.CodeNotAvailable)
	}
}
