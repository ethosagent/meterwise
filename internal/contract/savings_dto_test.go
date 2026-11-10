package contract_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestSavingsDTOJSONShape(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want []string
	}{
		{"hero", contract.SavingsHero{}, []string{"likelySavedTokens", "likelySavedUsd", "paidUsd", "savedBp", "savedTokens", "savedUsd", "unpricedCalls", "wouldHavePaidUsd", "wouldSaveTokens", "wouldSaveUsd"}},
		{"day", contract.SavingsDay{}, []string{"calls", "day", "likelySavedUsd", "paidUsd", "savedUsd", "sessions", "wouldHavePaidUsd"}},
		{"cause", contract.CauseView{}, []string{"cause", "confidence", "fires", "kind", "likelySavedTokens", "likelySavedUsd", "mode", "savedTokens", "savedUsd", "wouldSaveTokens", "wouldSaveUsd"}},
		{"causeCard", contract.CauseCard{}, []string{"cause", "confidence", "fires", "kind", "likelySavedTokens", "likelySavedUsd", "mode", "savedTokens", "savedUsd", "topClient", "topPath", "topPathHash", "topRepeatCount", "topTool", "wouldSaveTokens", "wouldSaveUsd"}},
		{"upsell", contract.UpsellView{}, []string{"cause", "fires", "wouldSaveTokens", "wouldSaveUsd"}},
		{"overview", contract.SavingsOverview{}, []string{"byCause", "days", "firstCallAt", "from", "hero", "range", "state", "timezone", "to", "upsell"}},
		{"client", contract.ClientRow{}, []string{"avgCallsPerSession", "blocked", "calls", "client", "likelySavedUsd", "prevAvgCallsPerSession", "savedBp", "savedUsd", "sessions", "spentUsd"}},
		{"suggestion", contract.Suggestion{}, []string{"count", "monitor", "path", "pathHash", "text", "tool"}},
		{"causes", contract.SavingsCauses{}, []string{"causes", "clients", "from", "range", "suggestions", "timezone", "to"}},
		{"save", contract.SaveRow{}, []string{"action", "callId", "callIndex", "client", "confidence", "estSavedTokens", "estSavedUsd", "eventId", "model", "monitor", "sessionId", "ts"}},
		{"saveList", contract.SaveList{}, []string{"items", "nextCursor"}},
		{"insight", contract.SessionInsight{}, []string{"repeats", "suggestion"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := jsonKeys(t, c.v); !slices.Equal(got, c.want) {
				t.Errorf("keys = %v\nwant   %v", got, c.want)
			}
		})
	}
}

func TestSavingsMoneyIsDecimalString(t *testing.T) {
	bp := 1000
	b, err := json.Marshal(contract.SavingsHero{SavedUsd: 1_000_000, LikelySavedUsd: 500_000, SavedBp: &bp})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{`"savedUsd":"1.000000"`, `"likelySavedUsd":"0.500000"`, `"savedBp":1000`, `"wouldHavePaidUsd":"0.000000"`} {
		if !strings.Contains(string(b), w) {
			t.Errorf("%s missing %s", b, w)
		}
	}
	if strings.Contains(string(b), "1.500000") {
		t.Errorf("a field sums certain and likely: %s", b)
	}
}

// TestSavingsNoSummedField: every savings-named field carries one class (Saved, LikelySaved, WouldSave)
// and none is a total; the only combined money is wouldHavePaid (paid + certain).
func TestSavingsNoSummedField(t *testing.T) {
	allowed := regexpSet("savedusd", "likelysavedusd", "wouldsaveusd", "savedtokens", "likelysavedtokens", "wouldsavetokens",
		"estsavedusd", "esttokens", "estsavedtokens", "savedbp", "savedusdtrim", "savedusdrouter")
	for _, v := range []any{contract.SavingsHero{}, contract.SavingsDay{}, contract.CauseView{}, contract.UpsellView{},
		contract.ClientRow{}, contract.SaveRow{}, contract.SavingsOverview{}, contract.SavingsCauses{}, contract.CauseCard{},
		contract.TrimStats{}, contract.RouterStats{}, contract.MemoryStats{}, contract.CallView{}, contract.SessionView{}} {
		for _, k := range jsonKeys(t, v) {
			lk := strings.ToLower(k)
			if !strings.Contains(lk, "sav") {
				continue
			}
			if !allowed[lk] {
				t.Errorf("%T field %q is not a single savings class", v, k)
			}
			if strings.Contains(lk, "total") || strings.Contains(lk, "combined") || strings.Contains(lk, "and") {
				t.Errorf("%T field %q reads as a sum", v, k)
			}
		}
	}
}

func regexpSet(s ...string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, k := range s {
		m[k] = true
	}
	return m
}

func TestSavingsDTOsRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, d := range contract.DTOs() {
		have[d.Name] = true
	}
	for _, n := range []string{"SavingsHero", "SavingsDay", "CauseView", "UpsellView", "SavingsOverview", "ClientRow", "Suggestion",
		"SavingsCauses", "SaveRow", "SaveList", "SessionInsight"} {
		if !have[n] {
			t.Errorf("DTOs() lacks %s", n)
		}
	}
}

func TestSessionDetailHasInsight(t *testing.T) {
	if !slices.Contains(jsonKeys(t, contract.SessionDetail{}), "insight") {
		t.Error("SessionDetail has no insight field")
	}
}

func TestValidSessionOutcome(t *testing.T) {
	for o, want := range map[string]bool{"": true, "completed": true, "abandoned": true, "open": true, "Open": false, "x": false} {
		if got := contract.ValidSessionOutcome(o); got != want {
			t.Errorf("ValidSessionOutcome(%q) = %v", o, got)
		}
	}
}

func TestParseSavingsRange(t *testing.T) {
	now := time.Date(2026, 11, 10, 15, 4, 0, 0, time.UTC)
	cases := []struct {
		name, rng, from, to string
		wantRange           string
		wantFrom, wantTo    string
		wantDays            int
		badField            string
	}{
		{"default is 7d", "", "", "", "7d", "2026-11-04", "2026-11-10", 7, ""},
		{"7d", "7d", "", "", "7d", "2026-11-04", "2026-11-10", 7, ""},
		{"30d", "30d", "", "", "30d", "2026-10-12", "2026-11-10", 30, ""},
		{"custom one day", "custom", "2026-11-10", "2026-11-10", "custom", "2026-11-10", "2026-11-10", 1, ""},
		{"custom 90 days", "custom", "2026-08-13", "2026-11-10", "custom", "2026-08-13", "2026-11-10", 90, ""},
		{"custom 91 days", "custom", "2026-08-12", "2026-11-10", "", "", "", 0, "from"},
		{"unknown range", "14d", "", "", "", "", "", 0, "range"},
		{"upper case", "7D", "", "", "", "", "", 0, "range"},
		{"from without custom", "7d", "2026-11-01", "", "", "", "", 0, "from"},
		{"to without custom", "30d", "", "2026-11-01", "", "", "", 0, "to"},
		{"custom missing from", "custom", "", "2026-11-01", "", "", "", 0, "from"},
		{"custom missing to", "custom", "2026-11-01", "", "", "", "", 0, "to"},
		{"bad from", "custom", "11/01/2026", "2026-11-02", "", "", "", 0, "from"},
		{"bad to", "custom", "2026-11-01", "2026-13-02", "", "", "", 0, "to"},
		{"from after to", "custom", "2026-11-05", "2026-11-04", "", "", "", 0, "from"},
		{"to in future", "custom", "2026-11-05", "2026-11-11", "", "", "", 0, "to"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := contract.ParseSavingsRange(c.rng, c.from, c.to, now)
			if c.badField != "" {
				var ce *contract.Error
				if !errors.As(err, &ce) || ce.Code != contract.CodeValidationFailed || !strings.HasPrefix(strings.Join(ce.Details, ";"), c.badField+":") {
					t.Fatalf("err = %v, want validation error on %q", err, c.badField)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.Range != c.wantRange || r.FromDay() != c.wantFrom || r.ToDay() != c.wantTo || r.Days() != c.wantDays {
				t.Errorf("got %s %s..%s %dd", r.Range, r.FromDay(), r.ToDay(), r.Days())
			}
		})
	}
}
