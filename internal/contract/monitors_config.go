package contract

import (
	"encoding/json"
	"errors"
	"strconv"
)

// MonitorConfig is the typed per-workspace monitor settings: integer thresholds and percentages, no floats.
type MonitorConfig struct {
	DuplicateToolCall DuplicateToolCallConfig `json:"duplicate_tool_call"`
	RepeatedRead      RepeatedReadConfig      `json:"repeated_read"`
	NoProgress        NoProgressConfig        `json:"no_progress"`
	StepBudget        StepBudgetConfig        `json:"step_budget"`
	ReasoningLoop     ReasoningLoopConfig     `json:"reasoning_loop"`
	SecondGuessing    SecondGuessingConfig    `json:"second_guessing"`
}

// DuplicateToolCallConfig tunes duplicate_tool_call.
type DuplicateToolCallConfig struct {
	Mode       MonitorMode `json:"mode"`
	MaxRepeats int         `json:"maxRepeats"`
	LookBack   int         `json:"lookBack"`
	FuzzyPct   int         `json:"fuzzyPct"`
	ABAB       bool        `json:"abab"`
}

// RepeatedReadConfig tunes repeated_read.
type RepeatedReadConfig struct {
	Mode       MonitorMode `json:"mode"`
	MinReads   int         `json:"minReads"`
	BlockAfter int         `json:"blockAfter"`
}

// NoProgressConfig tunes no_progress.
type NoProgressConfig struct {
	Mode          MonitorMode `json:"mode"`
	SimilarityPct int         `json:"similarityPct"`
}

// StepBudgetConfig tunes step_budget.
type StepBudgetConfig struct {
	Mode     MonitorMode `json:"mode"`
	MaxSteps int         `json:"maxSteps"`
}

// ReasoningLoopConfig tunes reasoning_loop.
type ReasoningLoopConfig struct {
	Mode          MonitorMode `json:"mode"`
	ShingleTokens int         `json:"shingleTokens"`
}

// SecondGuessingConfig tunes second_guessing.
type SecondGuessingConfig struct {
	Mode       MonitorMode `json:"mode"`
	MinPhrases int         `json:"minPhrases"`
}

// DefaultMonitorConfig has every monitor in observe mode with the stock thresholds.
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		DuplicateToolCall: DuplicateToolCallConfig{Mode: ModeObserve, MaxRepeats: 2, LookBack: 5},
		RepeatedRead:      RepeatedReadConfig{Mode: ModeObserve, MinReads: 3},
		NoProgress:        NoProgressConfig{Mode: ModeObserve, SimilarityPct: 95},
		StepBudget:        StepBudgetConfig{Mode: ModeObserve, MaxSteps: 40},
		ReasoningLoop:     ReasoningLoopConfig{Mode: ModeObserve, ShingleTokens: 40},
		SecondGuessing:    SecondGuessingConfig{Mode: ModeObserve, MinPhrases: 2},
	}
}

type modeRule struct {
	path string
	p    *MonitorMode
}

type intRule struct {
	path     string
	p        *int
	def      int
	min, max int
}

// modes lists the mode fields in MonitorNames order, keyed by field path.
func (c *MonitorConfig) modes() []modeRule {
	return []modeRule{
		{"duplicate_tool_call.mode", &c.DuplicateToolCall.Mode}, {"repeated_read.mode", &c.RepeatedRead.Mode},
		{"no_progress.mode", &c.NoProgress.Mode}, {"step_budget.mode", &c.StepBudget.Mode},
		{"reasoning_loop.mode", &c.ReasoningLoop.Mode}, {"second_guessing.mode", &c.SecondGuessing.Mode},
	}
}

// rules lists the integer fields in a fixed order; the defaults come from DefaultMonitorConfig's values.
func (c *MonitorConfig) rules() []intRule {
	return []intRule{
		{"duplicate_tool_call.maxRepeats", &c.DuplicateToolCall.MaxRepeats, 2, 2, 100},
		{"duplicate_tool_call.lookBack", &c.DuplicateToolCall.LookBack, 5, 1, 50},
		{"duplicate_tool_call.fuzzyPct", &c.DuplicateToolCall.FuzzyPct, 0, 0, 100},
		{"repeated_read.minReads", &c.RepeatedRead.MinReads, 3, 2, 100},
		{"repeated_read.blockAfter", &c.RepeatedRead.BlockAfter, 0, 0, 1000},
		{"no_progress.similarityPct", &c.NoProgress.SimilarityPct, 95, 50, 100},
		{"step_budget.maxSteps", &c.StepBudget.MaxSteps, 40, 1, 10000},
		{"reasoning_loop.shingleTokens", &c.ReasoningLoop.ShingleTokens, 40, 3, 500},
		{"second_guessing.minPhrases", &c.SecondGuessing.MinPhrases, 2, 1, 20},
	}
}

// sanitize resets every unknown mode and out-of-range threshold to its default.
func (c *MonitorConfig) sanitize() {
	for _, m := range c.modes() {
		if !m.p.Valid() {
			*m.p = ModeObserve
		}
	}
	for _, r := range c.rules() {
		if *r.p < r.min || *r.p > r.max {
			*r.p = r.def
		}
	}
}

// MonitorConfigFrom builds a config from whatever settings["monitors"] held: a MonitorConfig, JSON text or bytes,
// or a decoded map. Missing, malformed and partial input is filled with defaults, field by field.
func MonitorConfigFrom(v any) MonitorConfig {
	var raw []byte
	switch t := v.(type) {
	case nil:
		return DefaultMonitorConfig()
	case MonitorConfig:
		t.sanitize()
		return t
	case *MonitorConfig:
		if t == nil {
			return DefaultMonitorConfig()
		}
		return MonitorConfigFrom(*t)
	case string:
		raw = []byte(t)
	case []byte:
		raw = t
	case json.RawMessage:
		raw = t
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return DefaultMonitorConfig()
		}
		raw = b
	}
	cfg := DefaultMonitorConfig()
	// A wrong-typed field is skipped by Unmarshal and the rest still decode; only a syntax error aborts,
	// and then nothing was written.
	if err := json.Unmarshal(raw, &cfg); err != nil {
		var te *json.UnmarshalTypeError
		if !errors.As(err, &te) {
			return DefaultMonitorConfig()
		}
	}
	cfg.sanitize()
	return cfg
}

// Attr is the canonical JSON of c (fixed field order), the form stored in Tenant.Attrs["monitors"].
func (c MonitorConfig) Attr() string {
	b, err := json.Marshal(c)
	if err != nil { // unreachable: only strings, ints and bools
		return ""
	}
	return string(b)
}

// ParseMonitorAttr is the inverse of Attr. An empty or bad string yields the defaults.
func ParseMonitorAttr(s string) MonitorConfig {
	if s == "" {
		return DefaultMonitorConfig()
	}
	return MonitorConfigFrom(s)
}

// Validate rejects an unknown mode or an out-of-range threshold. The error is a validation Error
// whose details name the field path, such as "step_budget.maxSteps: must be between 1 and 10000".
func (c MonitorConfig) Validate() error {
	for _, m := range c.modes() {
		if !m.p.Valid() {
			return Invalid(m.path, "must be one of observe, note, block")
		}
	}
	for _, r := range c.rules() {
		if *r.p < r.min || *r.p > r.max {
			return Invalid(r.path, "must be between "+strconv.Itoa(r.min)+" and "+strconv.Itoa(r.max))
		}
	}
	return nil
}
