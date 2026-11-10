package contract

// MemoryMode is what memory may do: off, observe (record would-hint) or hint (inject a hint line).
type MemoryMode string

// The three memory modes.
const (
	MemoryModeOff     MemoryMode = "off"
	MemoryModeObserve MemoryMode = "observe"
	MemoryModeHint    MemoryMode = "hint"
)

// Valid reports whether m is one of the three modes.
func (m MemoryMode) Valid() bool {
	return m == MemoryModeOff || m == MemoryModeObserve || m == MemoryModeHint
}

// MemoryConfig is the per-workspace memory setting (workspaces.settings.memory). Model and EmbedModel "" mean the
// environment's (MEMORY_MODEL, MEMORY_EMBED_MODEL).
type MemoryConfig struct {
	Mode               MemoryMode `json:"mode"`
	ThresholdBp        int        `json:"thresholdBp"`
	MaxHintsPerSession int        `json:"maxHintsPerSession"`
	Model              string     `json:"model"`
	EmbedModel         string     `json:"embedModel"`
}

// DefaultMemoryConfig is off, similarity 8200 bp, one hint per session.
func DefaultMemoryConfig() MemoryConfig {
	return MemoryConfig{Mode: MemoryModeOff, ThresholdBp: 8200, MaxHintsPerSession: 1}
}

func (c *MemoryConfig) sanitize() {
	if !c.Mode.Valid() {
		c.Mode = MemoryModeOff
	}
	intInRange(&c.ThresholdBp, 8200, 5000, 10000)
	intInRange(&c.MaxHintsPerSession, 1, 1, 3)
	if len(c.Model) > MaxModelNameLen {
		c.Model = ""
	}
	if len(c.EmbedModel) > MaxModelNameLen {
		c.EmbedModel = ""
	}
}

// MemoryConfigFrom builds a config from whatever settings["memory"] held; see settingFrom.
func MemoryConfigFrom(v any) MemoryConfig {
	return settingFrom(v, DefaultMemoryConfig, (*MemoryConfig).sanitize)
}

// Attr is the canonical JSON of c, the form stored in Tenant.Attrs["memory"].
func (c MemoryConfig) Attr() string { return attrOf(c) }

// ParseMemoryAttr is the inverse of Attr. An empty or bad string yields the defaults.
func ParseMemoryAttr(s string) MemoryConfig {
	if s == "" {
		return DefaultMemoryConfig()
	}
	return MemoryConfigFrom(s)
}

// Validate rejects an unknown mode, an out-of-range number or an over-long model name; the error names the field.
func (c MemoryConfig) Validate() error {
	switch {
	case !c.Mode.Valid():
		return Invalid("mode", "must be one of off, observe, hint")
	case outside(c.ThresholdBp, 5000, 10000):
		return Invalid("thresholdBp", "must be between 5000 and 10000")
	case outside(c.MaxHintsPerSession, 1, 3):
		return Invalid("maxHintsPerSession", "must be between 1 and 3")
	case len(c.Model) > MaxModelNameLen:
		return Invalid("model", "at most 128 bytes")
	case len(c.EmbedModel) > MaxModelNameLen:
		return Invalid("embedModel", "at most 128 bytes")
	}
	return nil
}

// MemoryConfigPatch is the PATCH /api/workspaces/{id}/memory body: any subset, merged field by field.
type MemoryConfigPatch struct {
	Mode               *MemoryMode `json:"mode,omitempty"`
	ThresholdBp        *int        `json:"thresholdBp,omitempty"`
	MaxHintsPerSession *int        `json:"maxHintsPerSession,omitempty"`
	Model              *string     `json:"model,omitempty"`
	EmbedModel         *string     `json:"embedModel,omitempty"`
}

// Apply returns c with every field the patch names replaced; callers then Validate.
func (p MemoryConfigPatch) Apply(c MemoryConfig) MemoryConfig {
	setIf(&c.Mode, p.Mode)
	setIf(&c.ThresholdBp, p.ThresholdBp)
	setIf(&c.MaxHintsPerSession, p.MaxHintsPerSession)
	setIf(&c.Model, p.Model)
	setIf(&c.EmbedModel, p.EmbedModel)
	return c
}

// Touched lists the fields the patch names, in struct order.
func (p MemoryConfigPatch) Touched() []string {
	return fieldList([]string{"mode", "thresholdBp", "maxHintsPerSession", "model", "embedModel"},
		[]bool{p.Mode != nil, p.ThresholdBp != nil, p.MaxHintsPerSession != nil, p.Model != nil, p.EmbedModel != nil})
}
