package contract

import "reflect"

// MemorySettings is GET and PATCH /api/workspaces/{id}/memory. DisabledReason (no_key|privacy|budget) is set while
// Enabled is false; privacy means capture is not allowed for the workspace (non-negotiable 3, A4).
type MemorySettings struct {
	Config         MemoryConfig `json:"config"`
	Enabled        bool         `json:"enabled"`
	DisabledReason *string      `json:"disabledReason"`
}

// Reasons memory is disabled.
const (
	MemoryDisabledNoKey   = "no_key"
	MemoryDisabledPrivacy = "privacy"
	MemoryDisabledBudget  = "budget"
)

// MemoryStats is GET /api/workspaces/{id}/memory/stats?range=. LikelySavedUsd is a likely number and appears in no
// certain total; SelfUsageUsdMonth is what memory's own model calls cost this month.
type MemoryStats struct {
	Range             string     `json:"range"`
	From              string     `json:"from"`
	To                string     `json:"to"`
	Mode              MemoryMode `json:"mode"`
	Traces            int64      `json:"traces"`
	HintedSessions    int64      `json:"hintedSessions"`
	Hits              int64      `json:"hits"`
	ObservedHits      int64      `json:"observedHits"`
	Helpful           int64      `json:"helpful"`
	Irrelevant        int64      `json:"irrelevant"`
	LikelySavedUsd    MicroUSD   `json:"likelySavedUsd"`
	SelfUsageUsdMonth MicroUSD   `json:"selfUsageUsdMonth"`
	BudgetPaused      bool       `json:"budgetPaused"`
}

// TraceSummary is one remembered solution in a list.
type TraceSummary struct {
	ID           string   `json:"id"`
	SessionID    string   `json:"sessionId"`
	TaskSummary  string   `json:"taskSummary"`
	Files        []string `json:"files"`
	Uses         int64    `json:"uses"`
	Upvotes      int64    `json:"upvotes"`
	Downvotes    int64    `json:"downvotes"`
	ConfidenceBp int      `json:"confidenceBp"`
	LastUsedAt   *string  `json:"lastUsedAt"`
	CreatedAt    string   `json:"createdAt"`
}

// TraceList is GET /api/workspaces/{id}/traces: NextCursor is null on the last page.
type TraceList struct {
	Items      []TraceSummary `json:"items"`
	NextCursor *string        `json:"nextCursor"`
}

// TraceSignature is what a trace is matched on.
type TraceSignature struct {
	Repo   *string  `json:"repo"`
	Files  []string `json:"files"`
	Errors []string `json:"errors"`
	Tools  []string `json:"tools"`
}

// TraceDetail is GET /api/workspaces/{id}/traces/{traceId}: the summary's fields plus the distilled solution.
// HelpedSessions has at most 50 session ids.
type TraceDetail struct {
	TraceSummary
	Observations   []string       `json:"observations"`
	Approach       string         `json:"approach"`
	Fix            string         `json:"fix"`
	Signature      TraceSignature `json:"signature"`
	HelpedSessions []string       `json:"helpedSessions"`
}

// Memory hit feedback values.
const (
	MemoryFeedbackHelpful    = "helpful"
	MemoryFeedbackIrrelevant = "irrelevant"
)

// ValidMemoryFeedback reports whether fb is nil (clear) or one of the two values.
func ValidMemoryFeedback(fb *string) bool {
	return fb == nil || *fb == MemoryFeedbackHelpful || *fb == MemoryFeedbackIrrelevant
}

// MemoryHitFeedbackRequest is the POST .../memory-hits/{hitId}/feedback body; null (or missing) clears it.
type MemoryHitFeedbackRequest struct {
	Feedback *string `json:"feedback"`
}

// MemoryHitView is one hint decision. Hint is re-rendered from the trace, never stored as sent text.
type MemoryHitView struct {
	ID              string     `json:"id"`
	CallID          string     `json:"callId"`
	TraceID         string     `json:"traceId"`
	SourceSessionID string     `json:"sourceSessionId"`
	SimilarityBp    int        `json:"similarityBp"`
	Mode            MemoryMode `json:"mode"`
	Injected        bool       `json:"injected"`
	Hint            string     `json:"hint"`
	Feedback        *string    `json:"feedback"`
	EstSavedUsd     MicroUSD   `json:"estSavedUsd"`
}

// ForgetResult is what DELETE .../traces carries to its audit row (memory.forget {count}); the 204 sends no body.
type ForgetResult struct {
	Count int64 `json:"count"`
}

func memoryDTOs() []DTO {
	return []DTO{
		{"MemoryConfig", reflect.TypeFor[MemoryConfig]()},
		{"MemoryConfigPatch", reflect.TypeFor[MemoryConfigPatch]()},
		{"MemorySettings", reflect.TypeFor[MemorySettings]()},
		{"MemoryStats", reflect.TypeFor[MemoryStats]()},
		{"TraceSummary", reflect.TypeFor[TraceSummary]()},
		{"TraceList", reflect.TypeFor[TraceList]()},
		{"TraceSignature", reflect.TypeFor[TraceSignature]()},
		{"TraceDetail", reflect.TypeFor[TraceDetail]()},
		{"MemoryHitFeedbackRequest", reflect.TypeFor[MemoryHitFeedbackRequest]()},
		{"MemoryHitView", reflect.TypeFor[MemoryHitView]()},
		{"ForgetResult", reflect.TypeFor[ForgetResult]()},
	}
}
