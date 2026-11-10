package contract

import "strconv"

// Feedback values a user may put on a monitor event; anything else (or null) clears it.
const (
	FeedbackCorrect = "correct"
	FeedbackWrong   = "wrong"
)

// ValidFeedback reports whether fb is nil (clear) or one of the two feedback values.
func ValidFeedback(fb *string) bool {
	return fb == nil || *fb == FeedbackCorrect || *fb == FeedbackWrong
}

// PricedVerdict is a verdict the ledger has priced: the estimator's token saving, its USD value at the
// call's start and how sure it is. Only the ledger builds it (ruling R6); the store writes it.
type PricedVerdict struct {
	MonitorVerdict
	EstTokens  int64
	EstUSD     MicroUSD
	Confidence Confidence
}

// CountsAsSaved reports whether the verdict's estimate belongs in a savings number: the monitor acted
// (note or block). An observe verdict is would-save and belongs in neither (ruling R6).
func (v PricedVerdict) CountsAsSaved() bool { return v.Action != ModeObserve }

// MonitorStatRow is one monitor's counts over a window, as the store aggregates them. Saved and Likely are
// the two savings numbers (never summed); WouldSave is what observe-mode events would have saved.
type MonitorStatRow struct {
	Monitor         MonitorName
	Fires           int64
	Blocked         int64
	Noted           int64
	Observed        int64
	EstSavedTokens  int64
	Saved           MicroUSD
	Likely          MicroUSD
	WouldSave       MicroUSD
	FeedbackCorrect int64
	FeedbackWrong   int64
}

// MonitorEventView is one monitor event as the API sends it. EstSavedUsd is money (a decimal string).
type MonitorEventView struct {
	ID             string          `json:"id"`
	CallID         string          `json:"callId"`
	CallIndex      int             `json:"callIndex"`
	TS             string          `json:"ts"`
	Monitor        MonitorName     `json:"monitor"`
	Mode           MonitorMode     `json:"mode"`
	Action         MonitorMode     `json:"action"`
	Evidence       MonitorEvidence `json:"evidence"`
	EstSavedTokens int64           `json:"estSavedTokens"`
	EstSavedUsd    MicroUSD        `json:"estSavedUsd"`
	Confidence     Confidence      `json:"confidence"`
	Feedback       *string         `json:"feedback"` // null|correct|wrong
}

// MonitorFeedbackRequest is the POST .../monitor-events/{eventId}/feedback body. A null (or missing)
// feedback clears it; any other value than "correct" or "wrong" is a 422.
type MonitorFeedbackRequest struct {
	Feedback *string `json:"feedback"`
}

// MonitorStatsRowView is one monitor of the stats response. Mode is the workspace's current setting.
// FalsePositiveRate is wrong/(correct+wrong), null until there is feedback.
type MonitorStatsRowView struct {
	Monitor           MonitorName `json:"monitor"`
	Mode              MonitorMode `json:"mode"`
	Fires             int64       `json:"fires"`
	Blocked           int64       `json:"blocked"`
	Noted             int64       `json:"noted"`
	Observed          int64       `json:"observed"`
	EstSavedTokens    int64       `json:"estSavedTokens"`
	SavedUsd          MicroUSD    `json:"savedUsd"`
	LikelySavedUsd    MicroUSD    `json:"likelySavedUsd"`
	WouldSaveUsd      MicroUSD    `json:"wouldSaveUsd"`
	FeedbackCorrect   int64       `json:"feedbackCorrect"`
	FeedbackWrong     int64       `json:"feedbackWrong"`
	FalsePositiveRate *float64    `json:"falsePositiveRate"`
}

// MonitorStatsView is GET /api/workspaces/{id}/monitors/stats?range=7d|30d. SavedUsd and LikelySavedUsd
// are separate numbers and never a total; WouldSaveUsd is observe-mode events only. Monitors has the six
// monitors in their fixed order, zero-filled.
type MonitorStatsView struct {
	Range          string                `json:"range"`
	SavedUsd       MicroUSD              `json:"savedUsd"`
	LikelySavedUsd MicroUSD              `json:"likelySavedUsd"`
	WouldSaveUsd   MicroUSD              `json:"wouldSaveUsd"`
	Monitors       []MonitorStatsRowView `json:"monitors"`
}

// MonitorConfigPatch is the PATCH /api/workspaces/{id}/monitors body: any subset of the config, merged
// into the stored one field by field. An unknown key is a 422.
type MonitorConfigPatch struct {
	DuplicateToolCall *DuplicateToolCallPatch `json:"duplicate_tool_call,omitempty"`
	RepeatedRead      *RepeatedReadPatch      `json:"repeated_read,omitempty"`
	NoProgress        *NoProgressPatch        `json:"no_progress,omitempty"`
	StepBudget        *StepBudgetPatch        `json:"step_budget,omitempty"`
	ReasoningLoop     *ReasoningLoopPatch     `json:"reasoning_loop,omitempty"`
	SecondGuessing    *SecondGuessingPatch    `json:"second_guessing,omitempty"`
}

// DuplicateToolCallPatch is a partial DuplicateToolCallConfig.
type DuplicateToolCallPatch struct {
	Mode       *MonitorMode `json:"mode,omitempty"`
	MaxRepeats *int         `json:"maxRepeats,omitempty"`
	LookBack   *int         `json:"lookBack,omitempty"`
	FuzzyPct   *int         `json:"fuzzyPct,omitempty"`
	ABAB       *bool        `json:"abab,omitempty"`
}

// RepeatedReadPatch is a partial RepeatedReadConfig.
type RepeatedReadPatch struct {
	Mode       *MonitorMode `json:"mode,omitempty"`
	MinReads   *int         `json:"minReads,omitempty"`
	BlockAfter *int         `json:"blockAfter,omitempty"`
}

// NoProgressPatch is a partial NoProgressConfig.
type NoProgressPatch struct {
	Mode          *MonitorMode `json:"mode,omitempty"`
	SimilarityPct *int         `json:"similarityPct,omitempty"`
}

// StepBudgetPatch is a partial StepBudgetConfig.
type StepBudgetPatch struct {
	Mode     *MonitorMode `json:"mode,omitempty"`
	MaxSteps *int         `json:"maxSteps,omitempty"`
}

// ReasoningLoopPatch is a partial ReasoningLoopConfig.
type ReasoningLoopPatch struct {
	Mode          *MonitorMode `json:"mode,omitempty"`
	ShingleTokens *int         `json:"shingleTokens,omitempty"`
}

// SecondGuessingPatch is a partial SecondGuessingConfig.
type SecondGuessingPatch struct {
	Mode       *MonitorMode `json:"mode,omitempty"`
	MinPhrases *int         `json:"minPhrases,omitempty"`
}

func setIf[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}

// Apply returns c with every field the patch names replaced. It does not validate: callers run Validate
// on the result, so a bad value is a 422 naming its field path.
func (p MonitorConfigPatch) Apply(c MonitorConfig) MonitorConfig {
	if d := p.DuplicateToolCall; d != nil {
		setIf(&c.DuplicateToolCall.Mode, d.Mode)
		setIf(&c.DuplicateToolCall.MaxRepeats, d.MaxRepeats)
		setIf(&c.DuplicateToolCall.LookBack, d.LookBack)
		setIf(&c.DuplicateToolCall.FuzzyPct, d.FuzzyPct)
		setIf(&c.DuplicateToolCall.ABAB, d.ABAB)
	}
	if r := p.RepeatedRead; r != nil {
		setIf(&c.RepeatedRead.Mode, r.Mode)
		setIf(&c.RepeatedRead.MinReads, r.MinReads)
		setIf(&c.RepeatedRead.BlockAfter, r.BlockAfter)
	}
	if n := p.NoProgress; n != nil {
		setIf(&c.NoProgress.Mode, n.Mode)
		setIf(&c.NoProgress.SimilarityPct, n.SimilarityPct)
	}
	if s := p.StepBudget; s != nil {
		setIf(&c.StepBudget.Mode, s.Mode)
		setIf(&c.StepBudget.MaxSteps, s.MaxSteps)
	}
	if r := p.ReasoningLoop; r != nil {
		setIf(&c.ReasoningLoop.Mode, r.Mode)
		setIf(&c.ReasoningLoop.ShingleTokens, r.ShingleTokens)
	}
	if s := p.SecondGuessing; s != nil {
		setIf(&c.SecondGuessing.Mode, s.Mode)
		setIf(&c.SecondGuessing.MinPhrases, s.MinPhrases)
	}
	return c
}

// Touched lists the monitors the patch names, in MonitorNames order.
func (p MonitorConfigPatch) Touched() []MonitorName {
	var out []MonitorName
	for _, t := range []struct {
		name MonitorName
		set  bool
	}{
		{MonitorDuplicateToolCall, p.DuplicateToolCall != nil}, {MonitorRepeatedRead, p.RepeatedRead != nil},
		{MonitorNoProgress, p.NoProgress != nil}, {MonitorStepBudget, p.StepBudget != nil},
		{MonitorReasoningLoop, p.ReasoningLoop != nil}, {MonitorSecondGuessing, p.SecondGuessing != nil},
	} {
		if t.set {
			out = append(out, t.name)
		}
	}
	return out
}

// ModeOf is the configured mode of monitor m.
func (c MonitorConfig) ModeOf(m MonitorName) MonitorMode {
	switch m {
	case MonitorDuplicateToolCall:
		return c.DuplicateToolCall.Mode
	case MonitorRepeatedRead:
		return c.RepeatedRead.Mode
	case MonitorNoProgress:
		return c.NoProgress.Mode
	case MonitorStepBudget:
		return c.StepBudget.Mode
	case MonitorReasoningLoop:
		return c.ReasoningLoop.Mode
	case MonitorSecondGuessing:
		return c.SecondGuessing.Mode
	}
	return ModeObserve
}

// FalsePositiveRate is wrong/(correct+wrong) from the feedback counts, nil while there is no feedback.
func FalsePositiveRate(correct, wrong int64) *float64 {
	if correct+wrong == 0 {
		return nil
	}
	r := float64(wrong) / float64(correct+wrong)
	return &r
}

// MonitorEventCap is the most monitor events a session detail returns.
const MonitorEventCap = 1000

// monitorEventID formats a stored event id for the API (a string, so a bigint never loses precision in JS).
func monitorEventID(id int64) string { return strconv.FormatInt(id, 10) }

// View turns a stored event into its API view.
func (r MonitorEventRecord) View() MonitorEventView {
	return MonitorEventView{
		ID: monitorEventID(r.ID), CallID: r.CallID, CallIndex: r.CallIndex, TS: RFC3339(r.TS), Monitor: r.Monitor, Mode: r.Mode,
		Action: r.Action, Evidence: r.Evidence, EstSavedTokens: r.EstSavedToken, EstSavedUsd: r.EstSavedUSD, Confidence: r.Confidence,
		Feedback: r.Feedback,
	}
}
