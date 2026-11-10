package contract

import "reflect"

// RouterSettings is GET and PATCH /api/workspaces/{id}/router. Defaults are the priced default pairs, Suggestions
// pairs inferred from models seen in the workspace's calls (30 days) and Effective the pairs the router will use
// (the workspace map, else the defaults).
type RouterSettings struct {
	Config      RouterConfig `json:"config"`
	Defaults    []RoutePair  `json:"defaults"`
	Suggestions []RoutePair  `json:"suggestions"`
	Effective   []RoutePair  `json:"effective"`
}

// RouterStats is GET /api/workspaces/{id}/router/stats?range=. SavedUsd is certain (money actually not spent) and
// WouldSaveUsd is observe-mode only; never summed. RoutedBp is routed/calls in basis points, null without calls.
type RouterStats struct {
	Range         string           `json:"range"`
	From          string           `json:"from"`
	To            string           `json:"to"`
	Mode          RouterMode       `json:"mode"`
	Calls         int64            `json:"calls"`
	RoutedCalls   int64            `json:"routedCalls"`
	ObservedCalls int64            `json:"observedCalls"`
	Unavailable   int64            `json:"unavailable"`
	WorseFeedback int64            `json:"worseFeedback"`
	RoutedBp      *int             `json:"routedBp"`
	SavedUsd      MicroUSD         `json:"savedUsd"`
	WouldSaveUsd  MicroUSD         `json:"wouldSaveUsd"`
	Guardrail     RouterGuardrail  `json:"guardrail"`
	ByPair        []RouterPairStat `json:"byPair"`
}

// RouterGuardrail compares routed (cheap) and unrouted (strong) calls: fire rate of the monitors and the share of
// sessions that completed. Warn is true when the cheap model's fire rate exceeds the strong one's by over 200 bp.
type RouterGuardrail struct {
	CheapCalls        int64 `json:"cheapCalls"`
	StrongCalls       int64 `json:"strongCalls"`
	CheapFireBp       *int  `json:"cheapFireBp"`
	StrongFireBp      *int  `json:"strongFireBp"`
	CheapCompletedBp  *int  `json:"cheapCompletedBp"`
	StrongCompletedBp *int  `json:"strongCompletedBp"`
	Warn              bool  `json:"warn"`
}

// RouterPairStat is one routed pair over the range.
type RouterPairStat struct {
	Provider string   `json:"provider"`
	From     string   `json:"from"`
	To       string   `json:"to"`
	Calls    int64    `json:"calls"`
	SavedUsd MicroUSD `json:"savedUsd"`
}

// Router feedback verdicts.
const (
	RouterVerdictWorse = "worse"
	RouterVerdictFine  = "fine"
)

// ValidRouterVerdict reports whether v is nil (clear) or one of the two verdicts.
func ValidRouterVerdict(v *string) bool {
	return v == nil || *v == RouterVerdictWorse || *v == RouterVerdictFine
}

// RouterFeedbackRequest is the POST .../calls/{callId}/router-feedback body; null (or missing) clears it.
type RouterFeedbackRequest struct {
	Verdict *string `json:"verdict"`
}

// RouterFeedbackView is the feedback a call now carries.
type RouterFeedbackView struct {
	CallID  string  `json:"callId"`
	Verdict *string `json:"verdict"`
}

func routerDTOs() []DTO {
	return []DTO{
		{"RoutePair", reflect.TypeFor[RoutePair]()},
		{"RouterConfig", reflect.TypeFor[RouterConfig]()},
		{"RouterConfigPatch", reflect.TypeFor[RouterConfigPatch]()},
		{"RouterSettings", reflect.TypeFor[RouterSettings]()},
		{"RouterStats", reflect.TypeFor[RouterStats]()},
		{"RouterGuardrail", reflect.TypeFor[RouterGuardrail]()},
		{"RouterPairStat", reflect.TypeFor[RouterPairStat]()},
		{"RouterFeedbackRequest", reflect.TypeFor[RouterFeedbackRequest]()},
		{"RouterFeedbackView", reflect.TypeFor[RouterFeedbackView]()},
	}
}
