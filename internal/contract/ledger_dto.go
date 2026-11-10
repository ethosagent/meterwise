package contract

// SessionView is one session as the API sends it. Money is a decimal string (ruling R12); CostUsd is
// null when no call of the session could be priced.
type SessionView struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspaceId"`
	Source         string    `json:"source"` // header|predicted
	StartedAt      string    `json:"startedAt"`
	LastSeenAt     string    `json:"lastSeenAt"`
	ClosedAt       *string   `json:"closedAt"`
	Calls          int       `json:"calls"`
	InputTokens    int64     `json:"inputTokens"`
	OutputTokens   int64     `json:"outputTokens"`
	CostUsd        *MicroUSD `json:"costUsd"`
	SavedUsd       MicroUSD  `json:"savedUsd"`
	LikelySavedUsd MicroUSD  `json:"likelySavedUsd"`
	Outcome        string    `json:"outcome"` // completed|abandoned|open
	Client         *string   `json:"client"`
	TrimTokens     int64     `json:"trimTokens"`
	RoutedCalls    int       `json:"routedCalls"`
	Hints          int       `json:"hints"`
}

// CallView is one call of a session as the API sends it. CostUsd is null for a model with no price.
type CallView struct {
	ID              string    `json:"id"`
	CallIndex       int       `json:"callIndex"`
	StartedAt       string    `json:"startedAt"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	Wire            string    `json:"wire"`
	Stream          bool      `json:"stream"`
	Status          int       `json:"status"`
	Finish          string    `json:"finish"`
	InputTokens     int64     `json:"inputTokens"`
	OutputTokens    int64     `json:"outputTokens"`
	CachedTokens    int64     `json:"cachedTokens"`
	ReasoningTokens int64     `json:"reasoningTokens"`
	CostUsd         *MicroUSD `json:"costUsd"`
	TtfbMs          int64     `json:"ttfbMs"`
	DurationMs      int64     `json:"durationMs"`
	ToolCalls       []string  `json:"toolCalls"`
	Blocked         bool      `json:"blocked"`
	NotesInjected   int       `json:"notesInjected"`
	TrimAction      *string   `json:"trimAction"`
	TrimConfidence  *string   `json:"trimConfidence"`
	TrimTokens      int64     `json:"trimTokens"`
	SavedUsdTrim    MicroUSD  `json:"savedUsdTrim"`
	RouterAction    *string   `json:"routerAction"`
	RoutedFrom      *string   `json:"routedFrom"`
	RoutedTo        *string   `json:"routedTo"`
	RouterScoreBp   *int      `json:"routerScoreBp"`
	SavedUsdRouter  MicroUSD  `json:"savedUsdRouter"`
	HintInjected    bool      `json:"hintInjected"`
}

// SessionList is GET /api/workspaces/{id}/sessions: newest first, NextCursor null on the last page.
type SessionList struct {
	Items      []SessionView `json:"items"`
	NextCursor *string       `json:"nextCursor"`
}

// SessionDetail is GET /api/sessions/{id}: the session and its calls in call order. Truncated is true
// when the session has more calls than Calls holds (the detail returns at most 5000).
type SessionDetail struct {
	Session       SessionView        `json:"session"`
	Calls         []CallView         `json:"calls"`
	Truncated     bool               `json:"truncated"`
	MonitorEvents []MonitorEventView `json:"monitorEvents"` // call order, at most MonitorEventCap
	Insight       SessionInsight     `json:"insight"`
	MemoryHits    []MemoryHitView    `json:"memoryHits"` // [] until memory records hits (wave 2)
}

// StatsDayView is one UTC day of the stats response; Day is YYYY-MM-DD.
type StatsDayView struct {
	Day      string   `json:"day"`
	SpendUsd MicroUSD `json:"spendUsd"`
	Calls    int      `json:"calls"`
	Sessions int      `json:"sessions"`
}

// StatsView is GET /api/workspaces/{id}/stats?range=7d|30d. ByDay has one entry per day of the
// range, zero-filled, oldest first.
type StatsView struct {
	SpendUsd           MicroUSD       `json:"spendUsd"`
	Calls              int            `json:"calls"`
	Sessions           int            `json:"sessions"`
	AvgCallsPerSession float64        `json:"avgCallsPerSession"`
	ByDay              []StatsDayView `json:"byDay"`
	SavedUsd           MicroUSD       `json:"savedUsd"`       // certain savings of the range
	LikelySavedUsd     MicroUSD       `json:"likelySavedUsd"` // likely savings of the range; never summed with SavedUsd
}
