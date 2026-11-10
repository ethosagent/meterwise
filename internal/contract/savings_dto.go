package contract

import (
	"reflect"
	"time"
)

// SavingsHero is the headline of the savings dashboard. The three savings classes (certain, likely,
// would-save) each have their own field, tokens and USD alike, and no field holds a sum of classes
// (ADR 0023). WouldHavePaidUsd = PaidUsd + SavedUsd (certain only). SavedBp is SavedUsd/(PaidUsd+SavedUsd)
// in basis points (0-10000), null when that denominator is 0.
type SavingsHero struct {
	SavedUsd          MicroUSD `json:"savedUsd"`
	LikelySavedUsd    MicroUSD `json:"likelySavedUsd"`
	WouldSaveUsd      MicroUSD `json:"wouldSaveUsd"`
	PaidUsd           MicroUSD `json:"paidUsd"`
	WouldHavePaidUsd  MicroUSD `json:"wouldHavePaidUsd"`
	SavedTokens       int64    `json:"savedTokens"`
	LikelySavedTokens int64    `json:"likelySavedTokens"`
	WouldSaveTokens   int64    `json:"wouldSaveTokens"`
	SavedBp           *int     `json:"savedBp"`
	UnpricedCalls     int64    `json:"unpricedCalls"`
}

// SavingsDay is one UTC day of the chart; Day is YYYY-MM-DD. The days array is zero-filled.
type SavingsDay struct {
	Day              string   `json:"day"`
	PaidUsd          MicroUSD `json:"paidUsd"`
	SavedUsd         MicroUSD `json:"savedUsd"`
	LikelySavedUsd   MicroUSD `json:"likelySavedUsd"`
	WouldHavePaidUsd MicroUSD `json:"wouldHavePaidUsd"`
	Calls            int64    `json:"calls"`
	Sessions         int64    `json:"sessions"`
}

// CauseView is one monitor's contribution over the range. Confidence is the class the estimator gives
// the monitor. The Top* fields name the most frequent client, tool and path (null when none); TopPath is
// set only when a path was stored (rule R7), else TopPathHash holds the 8-hex prefix.
type CauseView struct {
	Monitor           MonitorName `json:"monitor"`
	Mode              MonitorMode `json:"mode"`
	Confidence        Confidence  `json:"confidence"`
	Fires             int64       `json:"fires"`
	SavedUsd          MicroUSD    `json:"savedUsd"`
	LikelySavedUsd    MicroUSD    `json:"likelySavedUsd"`
	WouldSaveUsd      MicroUSD    `json:"wouldSaveUsd"`
	SavedTokens       int64       `json:"savedTokens"`
	LikelySavedTokens int64       `json:"likelySavedTokens"`
	WouldSaveTokens   int64       `json:"wouldSaveTokens"`
	TopClient         *string     `json:"topClient"`
	TopTool           *string     `json:"topTool"`
	TopPath           *string     `json:"topPath"`
	TopPathHash       *string     `json:"topPathHash"`
	TopRepeatCount    int64       `json:"topRepeatCount"`
}

// UpsellView is an observe-mode monitor with would-save > 0, ordered by would-save descending.
type UpsellView struct {
	Monitor         MonitorName `json:"monitor"`
	WouldSaveUsd    MicroUSD    `json:"wouldSaveUsd"`
	WouldSaveTokens int64       `json:"wouldSaveTokens"`
	Fires           int64       `json:"fires"`
}

// Savings overview states.
const (
	SavingsNoCalls    = "no_calls"
	SavingsCollecting = "collecting"
	SavingsActive     = "active"
)

// SavingsOverview is GET /api/workspaces/{id}/savings/overview. Range is "7d", "30d" or "custom";
// From and To are inclusive UTC days (YYYY-MM-DD); Timezone is always "UTC"; State is one of the
// Savings* constants. ByCause has six rows in MonitorNames order.
type SavingsOverview struct {
	Range       string       `json:"range"`
	From        string       `json:"from"`
	To          string       `json:"to"`
	Timezone    string       `json:"timezone"`
	State       string       `json:"state"`
	FirstCallAt *string      `json:"firstCallAt"`
	Hero        SavingsHero  `json:"hero"`
	Days        []SavingsDay `json:"days"`
	ByCause     []CauseView  `json:"byCause"`
	Upsell      []UpsellView `json:"upsell"`
}

// ClientRow is one client family over the range; Client is "unknown" for a NULL client. SavedBp follows
// SavingsHero.SavedBp. PrevAvgCallsPerSession is the same-length window before the range, null if none.
type ClientRow struct {
	Client                 string   `json:"client"`
	Calls                  int64    `json:"calls"`
	Sessions               int64    `json:"sessions"`
	Blocked                int64    `json:"blocked"`
	SpentUsd               MicroUSD `json:"spentUsd"`
	SavedUsd               MicroUSD `json:"savedUsd"`
	LikelySavedUsd         MicroUSD `json:"likelySavedUsd"`
	SavedBp                *int     `json:"savedBp"`
	AvgCallsPerSession     float64  `json:"avgCallsPerSession"`
	PrevAvgCallsPerSession *float64 `json:"prevAvgCallsPerSession"`
}

// Suggestion is a fixed-template prompt hint. Path is set only when one was stored; otherwise Tool and
// the PathHash prefix name the target. It never carries message text.
type Suggestion struct {
	Monitor  MonitorName `json:"monitor"`
	Tool     string      `json:"tool"`
	Path     *string     `json:"path"`
	PathHash *string     `json:"pathHash"`
	Count    int64       `json:"count"`
	Text     string      `json:"text"`
}

// SavingsCauses is GET /api/workspaces/{id}/savings/causes.
type SavingsCauses struct {
	Range       string       `json:"range"`
	From        string       `json:"from"`
	To          string       `json:"to"`
	Timezone    string       `json:"timezone"`
	Causes      []CauseView  `json:"causes"`
	Clients     []ClientRow  `json:"clients"`
	Suggestions []Suggestion `json:"suggestions"`
}

// SaveRow is one acted event with a positive estimate (either confidence), for the biggest-saves list.
type SaveRow struct {
	EventID        string      `json:"eventId"`
	SessionID      string      `json:"sessionId"`
	CallID         string      `json:"callId"`
	CallIndex      int         `json:"callIndex"`
	TS             string      `json:"ts"`
	Monitor        MonitorName `json:"monitor"`
	Action         MonitorMode `json:"action"`
	Confidence     Confidence  `json:"confidence"`
	EstSavedUsd    MicroUSD    `json:"estSavedUsd"`
	EstSavedTokens int64       `json:"estSavedTokens"`
	Client         *string     `json:"client"`
	Model          string      `json:"model"`
}

// SaveList is GET /api/workspaces/{id}/savings/saves, ordered (est_saved_usd DESC, id DESC) with an opaque cursor.
type SaveList struct {
	Items      []SaveRow `json:"items"`
	NextCursor *string   `json:"nextCursor"`
}

// SessionInsight is SessionDetail.insight: repeated targets in the session and one suggestion (null if none).
type SessionInsight struct {
	Repeats    []Suggestion `json:"repeats"`
	Suggestion *string      `json:"suggestion"`
}

// ValidSessionOutcome reports whether o is "" (any) or a known outcome.
func ValidSessionOutcome(o string) bool {
	return o == "" || o == OutcomeCompleted || o == OutcomeAbandoned || o == OutcomeOpen
}

// MaxSavingsRangeDays is the longest custom range, in days, both ends included.
const MaxSavingsRangeDays = 90

// SavingsRange is a validated savings window: inclusive UTC days.
type SavingsRange struct {
	Range    string // "7d", "30d" or "custom"
	From, To time.Time
}

const dayLayout = "2006-01-02"

// Days is the number of UTC days in the window, both ends included.
func (r SavingsRange) Days() int { return int(r.To.Sub(r.From)/(24*time.Hour)) + 1 }

// FromDay renders the first day as YYYY-MM-DD.
func (r SavingsRange) FromDay() string { return r.From.Format(dayLayout) }

// ToDay renders the last day as YYYY-MM-DD.
func (r SavingsRange) ToDay() string { return r.To.Format(dayLayout) }

// ParseSavingsRange validates the range, from and to query values against now (ruling S5). "" means 7d.
// 7d and 30d end today (UTC) and take no from/to. custom needs both, as UTC days, from <= to <= today,
// at most MaxSavingsRangeDays. Errors are 422 validation_failed naming the field (range, from, to).
func ParseSavingsRange(rng, from, to string, now time.Time) (SavingsRange, error) {
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	switch rng {
	case "", "7d", "30d":
		if rng == "" {
			rng = "7d"
		}
		if from != "" {
			return SavingsRange{}, Invalid("from", "only allowed with range=custom")
		}
		if to != "" {
			return SavingsRange{}, Invalid("to", "only allowed with range=custom")
		}
		n := 7
		if rng == "30d" {
			n = 30
		}
		return SavingsRange{Range: rng, From: today.AddDate(0, 0, 1-n), To: today}, nil
	case "custom":
		return parseCustomRange(from, to, today)
	}
	return SavingsRange{}, Invalid("range", "must be 7d, 30d or custom")
}

func parseCustomRange(from, to string, today time.Time) (SavingsRange, error) {
	f, err := parseDay("from", from)
	if err != nil {
		return SavingsRange{}, err
	}
	t, err := parseDay("to", to)
	if err != nil {
		return SavingsRange{}, err
	}
	switch {
	case f.After(t):
		return SavingsRange{}, Invalid("from", "must not be after to")
	case t.After(today):
		return SavingsRange{}, Invalid("to", "must not be after today (UTC)")
	}
	r := SavingsRange{Range: "custom", From: f, To: t}
	if r.Days() > MaxSavingsRangeDays {
		return SavingsRange{}, Invalid("from", "range is at most 90 days")
	}
	return r, nil
}

func parseDay(field, v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, Invalid(field, "required with range=custom")
	}
	d, err := time.Parse(dayLayout, v)
	if err != nil {
		return time.Time{}, Invalid(field, "must be a YYYY-MM-DD date")
	}
	return d, nil
}

// savingsDTOs are the Plan 07 types, appended to DTOs in this order.
func savingsDTOs() []DTO {
	return []DTO{
		{"SavingsHero", reflect.TypeFor[SavingsHero]()},
		{"SavingsDay", reflect.TypeFor[SavingsDay]()},
		{"CauseView", reflect.TypeFor[CauseView]()},
		{"UpsellView", reflect.TypeFor[UpsellView]()},
		{"SavingsOverview", reflect.TypeFor[SavingsOverview]()},
		{"ClientRow", reflect.TypeFor[ClientRow]()},
		{"Suggestion", reflect.TypeFor[Suggestion]()},
		{"SavingsCauses", reflect.TypeFor[SavingsCauses]()},
		{"SaveRow", reflect.TypeFor[SaveRow]()},
		{"SaveList", reflect.TypeFor[SaveList]()},
		{"SessionInsight", reflect.TypeFor[SessionInsight]()},
	}
}
