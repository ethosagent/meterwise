package contract

import (
	"strconv"
)

// TrimMode is what trimming may do: off, observe (measure what it would trim), lossless or aggressive.
type TrimMode string

// The four trim modes.
const (
	TrimModeOff        TrimMode = "off"
	TrimModeObserve    TrimMode = "observe"
	TrimModeLossless   TrimMode = "lossless"
	TrimModeAggressive TrimMode = "aggressive"
)

// Valid reports whether m is one of the four modes.
func (m TrimMode) Valid() bool {
	return m == TrimModeOff || m == TrimModeObserve || m == TrimModeLossless || m == TrimModeAggressive
}

// Limits of the trim settings.
const (
	MaxNeverTrimTools   = 50
	MaxNeverTrimToolLen = 128
)

// TrimConfig is the per-workspace trim setting (workspaces.settings.trim). MinGainPct applies per tool-result
// region, never per request (R6, A5). Stages always has the five stages; schema_trim only acts in aggressive mode.
type TrimConfig struct {
	Mode           TrimMode           `json:"mode"`
	NeverTrimTools []string           `json:"neverTrimTools"`
	MaxFoldPct     int                `json:"maxFoldPct"`
	MinGainPct     int                `json:"minGainPct"`
	Stages         map[TrimStage]bool `json:"stages"`
}

func defaultTrimStages() map[TrimStage]bool {
	m := map[TrimStage]bool{}
	for _, s := range TrimStages() {
		m[s] = s != StageSchemaTrim
	}
	return m
}

// DefaultTrimConfig observes only, with every stage on except schema_trim.
func DefaultTrimConfig() TrimConfig {
	return TrimConfig{Mode: TrimModeObserve, NeverTrimTools: []string{}, MaxFoldPct: 90, MinGainPct: 2, Stages: defaultTrimStages()}
}

func (c *TrimConfig) sanitize() {
	if !c.Mode.Valid() {
		c.Mode = TrimModeObserve
	}
	intInRange(&c.MaxFoldPct, 90, 10, 100)
	intInRange(&c.MinGainPct, 2, 0, 50)
	c.NeverTrimTools = cleanStrings(c.NeverTrimTools, MaxNeverTrimTools, MaxNeverTrimToolLen)
	stages := defaultTrimStages()
	for s, on := range c.Stages {
		if s.Valid() {
			stages[s] = on
		}
	}
	c.Stages = stages
}

// TrimConfigFrom builds a config from whatever settings["trim"] held; see settingFrom.
func TrimConfigFrom(v any) TrimConfig {
	return settingFrom(v, DefaultTrimConfig, (*TrimConfig).sanitize)
}

// Attr is the canonical JSON of c, the form stored in Tenant.Attrs["trim"].
func (c TrimConfig) Attr() string { return attrOf(c) }

// ParseTrimAttr is the inverse of Attr. An empty or bad string yields the defaults.
func ParseTrimAttr(s string) TrimConfig {
	if s == "" {
		return DefaultTrimConfig()
	}
	return TrimConfigFrom(s)
}

// Validate rejects an unknown mode, stage or tool name and an out-of-range number; the error names the field path.
func (c TrimConfig) Validate() error {
	switch {
	case !c.Mode.Valid():
		return Invalid("mode", "must be one of off, observe, lossless, aggressive")
	case outside(c.MaxFoldPct, 10, 100):
		return Invalid("maxFoldPct", "must be between 10 and 100")
	case outside(c.MinGainPct, 0, 50):
		return Invalid("minGainPct", "must be between 0 and 50")
	case len(c.NeverTrimTools) > MaxNeverTrimTools:
		return Invalid("neverTrimTools", "at most "+strconv.Itoa(MaxNeverTrimTools)+" tools")
	}
	for i, t := range c.NeverTrimTools {
		if t == "" || len(t) > MaxNeverTrimToolLen {
			return Invalid("neverTrimTools["+strconv.Itoa(i)+"]", "must be 1-"+strconv.Itoa(MaxNeverTrimToolLen)+" bytes")
		}
	}
	for s := range c.Stages {
		if !s.Valid() {
			return Invalid("stages."+string(s), "unknown stage")
		}
	}
	return nil
}

// TrimConfigPatch is the PATCH /api/workspaces/{id}/trim body: any subset, merged field by field. Stages merges
// per stage. An unknown key is a 422.
type TrimConfigPatch struct {
	Mode           *TrimMode          `json:"mode,omitempty"`
	NeverTrimTools *[]string          `json:"neverTrimTools,omitempty"`
	MaxFoldPct     *int               `json:"maxFoldPct,omitempty"`
	MinGainPct     *int               `json:"minGainPct,omitempty"`
	Stages         map[TrimStage]bool `json:"stages,omitempty"`
}

// Apply returns c with every field the patch names replaced; callers then Validate.
func (p TrimConfigPatch) Apply(c TrimConfig) TrimConfig {
	setIf(&c.Mode, p.Mode)
	setIf(&c.NeverTrimTools, p.NeverTrimTools)
	setIf(&c.MaxFoldPct, p.MaxFoldPct)
	setIf(&c.MinGainPct, p.MinGainPct)
	if len(p.Stages) > 0 {
		merged := make(map[TrimStage]bool, len(c.Stages)+len(p.Stages))
		for s, on := range c.Stages {
			merged[s] = on
		}
		for s, on := range p.Stages {
			merged[s] = on
		}
		c.Stages = merged
	}
	return c
}

// Touched lists the fields the patch names, in struct order.
func (p TrimConfigPatch) Touched() []string {
	return fieldList([]string{"mode", "neverTrimTools", "maxFoldPct", "minGainPct", "stages"},
		[]bool{p.Mode != nil, p.NeverTrimTools != nil, p.MaxFoldPct != nil, p.MinGainPct != nil, len(p.Stages) > 0})
}
