package contract

// SavingsCause names one row of Savings by cause: the six monitors, then trim, router and memory.
type SavingsCause string

// The three causes beyond the monitors; a monitor's cause is its MonitorName.
const (
	CauseTrim   SavingsCause = "trim"
	CauseRouter SavingsCause = "router"
	CauseMemory SavingsCause = "memory"
)

// CauseKind says which feature a cause belongs to.
type CauseKind string

// The four cause kinds.
const (
	KindMonitor CauseKind = "monitor"
	KindTrim    CauseKind = "trim"
	KindRouter  CauseKind = "router"
	KindMemory  CauseKind = "memory"
)

// AllSavingsCauses returns the nine causes in their fixed order: the monitors, then trim, router, memory.
func AllSavingsCauses() []SavingsCause {
	out := make([]SavingsCause, 0, 9)
	for _, m := range MonitorNames() {
		out = append(out, SavingsCause(m))
	}
	return append(out, CauseTrim, CauseRouter, CauseMemory)
}

// CauseKinds returns the four kinds in a fixed order (monitor, trim, router, memory).
func CauseKinds() []CauseKind { return []CauseKind{KindMonitor, KindTrim, KindRouter, KindMemory} }

// Kind is the feature the cause belongs to; an unknown cause is a monitor's (it came from monitor_events).
func (c SavingsCause) Kind() CauseKind {
	switch c {
	case CauseTrim:
		return KindTrim
	case CauseRouter:
		return KindRouter
	case CauseMemory:
		return KindMemory
	}
	return KindMonitor
}

// Monitor is the monitor of a monitor cause; ok is false for trim, router and memory.
func (c SavingsCause) Monitor() (MonitorName, bool) {
	if c.Kind() != KindMonitor {
		return "", false
	}
	return MonitorName(c), true
}

// StepEventKind is the kind of one event card on a session step (emitted to TypeScript).
type StepEventKind string

// The four event-card kinds.
const (
	StepMonitorFire StepEventKind = "monitor_fire"
	StepTrim        StepEventKind = "trim"
	StepRoute       StepEventKind = "route"
	StepMemoryHint  StepEventKind = "memory_hint"
)

// StepEventKinds returns the four kinds in a fixed order.
func StepEventKinds() []StepEventKind {
	return []StepEventKind{StepMonitorFire, StepTrim, StepRoute, StepMemoryHint}
}

// CauseView is one cause's contribution over the range. Mode is the feature's current setting (a monitor mode, a
// trim, router or memory mode). Confidence is the class the estimator gives the cause. Saved, likely and would-save
// are three numbers, never a sum (ADR 0023); trim and router certain savings are in the headline, memory's are likely.
type CauseView struct {
	Cause             SavingsCause `json:"cause"`
	Kind              CauseKind    `json:"kind"`
	Mode              string       `json:"mode"`
	Confidence        Confidence   `json:"confidence"`
	Fires             int64        `json:"fires"`
	SavedUsd          MicroUSD     `json:"savedUsd"`
	LikelySavedUsd    MicroUSD     `json:"likelySavedUsd"`
	WouldSaveUsd      MicroUSD     `json:"wouldSaveUsd"`
	SavedTokens       int64        `json:"savedTokens"`
	LikelySavedTokens int64        `json:"likelySavedTokens"`
	WouldSaveTokens   int64        `json:"wouldSaveTokens"`
}

// CauseCard is a CauseView plus the monitor-only detail the Causes tab shows. The Top* fields name the most
// frequent client, tool and path (null when none); TopPath is set only when a path was stored (rule R7), else
// TopPathHash holds the 8-hex prefix. Only SavingsCauses.Causes carries cards; the overview's ByCause does not.
type CauseCard struct {
	CauseView
	TopClient      *string `json:"topClient"`
	TopTool        *string `json:"topTool"`
	TopPath        *string `json:"topPath"`
	TopPathHash    *string `json:"topPathHash"`
	TopRepeatCount int64   `json:"topRepeatCount"`
}

// UpsellView is an observe-mode cause with would-save > 0, ordered by would-save descending.
type UpsellView struct {
	Cause           SavingsCause `json:"cause"`
	WouldSaveUsd    MicroUSD     `json:"wouldSaveUsd"`
	WouldSaveTokens int64        `json:"wouldSaveTokens"`
	Fires           int64        `json:"fires"`
}
