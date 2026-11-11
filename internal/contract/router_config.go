package contract

import "strconv"

// RouterMode is what the router may do: off, observe (record would-route), conservative or aggressive.
type RouterMode string

// The four router modes.
const (
	RouterModeOff          RouterMode = "off"
	RouterModeObserve      RouterMode = "observe"
	RouterModeConservative RouterMode = "conservative"
	RouterModeAggressive   RouterMode = "aggressive"
)

// Valid reports whether m is one of the four modes.
func (m RouterMode) Valid() bool {
	return m == RouterModeOff || m == RouterModeObserve || m == RouterModeConservative || m == RouterModeAggressive
}

// Router limits and default thresholds (basis points of the 0-10000 simplicity score).
const (
	MaxRoutePairs              = 100
	MaxModelNameLen            = 128
	DefaultConservativeScoreBp = 2500
	DefaultAggressiveScoreBp   = 5000
)

// RoutePair maps one provider's model to the cheaper model it may be routed to.
type RoutePair struct {
	Provider string `json:"provider"`
	From     string `json:"from"`
	To       string `json:"to"`
}

func (p RoutePair) valid() bool {
	return p.Provider != "" && len(p.Provider) <= MaxModelNameLen && p.From != "" && len(p.From) <= MaxModelNameLen &&
		p.To != "" && len(p.To) <= MaxModelNameLen && p.From != p.To
}

// RouterConfig is the per-workspace router setting (workspaces.settings.router). ThresholdBp null means 2500 in
// conservative and 5000 in aggressive mode. Map null means the default map; a PATCH that sets map stores the
// workspace's own copy (R10).
type RouterConfig struct {
	Mode        RouterMode  `json:"mode"`
	ThresholdBp *int        `json:"thresholdBp"`
	Map         []RoutePair `json:"map" ts:"nullable"`
}

// DefaultRouterConfig is off, with the default threshold and the default map.
func DefaultRouterConfig() RouterConfig { return RouterConfig{Mode: RouterModeOff} }

// EffectiveThresholdBp is the score a request must stay at or under to be routed: the configured threshold, else
// the mode's default.
func (c RouterConfig) EffectiveThresholdBp() int {
	switch {
	case c.ThresholdBp != nil:
		return *c.ThresholdBp
	case c.Mode == RouterModeAggressive:
		return DefaultAggressiveScoreBp
	}
	return DefaultConservativeScoreBp
}

func (c *RouterConfig) sanitize() {
	if !c.Mode.Valid() {
		c.Mode = RouterModeOff
	}
	if c.ThresholdBp != nil && (*c.ThresholdBp < 1 || *c.ThresholdBp > 10000) {
		c.ThresholdBp = nil
	}
	if c.Map == nil {
		return
	}
	seen := map[[2]string]bool{}
	out := make([]RoutePair, 0, len(c.Map))
	for _, p := range c.Map {
		k := [2]string{p.Provider, p.From}
		if p.valid() && !seen[k] && len(out) < MaxRoutePairs {
			seen[k] = true
			out = append(out, p)
		}
	}
	c.Map = out
}

// RouterConfigFrom builds a config from whatever settings["router"] held; see settingFrom.
func RouterConfigFrom(v any) RouterConfig {
	return settingFrom(v, DefaultRouterConfig, (*RouterConfig).sanitize)
}

// Attr is the canonical JSON of c, the form stored in Tenant.Attrs["router"].
func (c RouterConfig) Attr() string { return attrOf(c) }

// ParseRouterAttr is the inverse of Attr. An empty or bad string yields the defaults.
func ParseRouterAttr(s string) RouterConfig {
	if s == "" {
		return DefaultRouterConfig()
	}
	return RouterConfigFrom(s)
}

// Validate rejects an unknown mode, an out-of-range threshold and a bad or duplicate pair; the error names the path.
func (c RouterConfig) Validate() error {
	if !c.Mode.Valid() {
		return Invalid("mode", "must be one of off, observe, conservative, aggressive")
	}
	if c.ThresholdBp != nil && (*c.ThresholdBp < 1 || *c.ThresholdBp > 10000) {
		return Invalid("thresholdBp", "must be between 1 and 10000")
	}
	if len(c.Map) > MaxRoutePairs {
		return Invalid("map", "at most "+strconv.Itoa(MaxRoutePairs)+" pairs")
	}
	seen := map[[2]string]bool{}
	for i, p := range c.Map {
		at := "map[" + strconv.Itoa(i) + "]"
		switch {
		case p.Provider == "" || len(p.Provider) > MaxModelNameLen:
			return Invalid(at+".provider", "must be 1-128 bytes")
		case p.From == "" || len(p.From) > MaxModelNameLen:
			return Invalid(at+".from", "must be 1-128 bytes")
		case p.To == "" || len(p.To) > MaxModelNameLen:
			return Invalid(at+".to", "must be 1-128 bytes")
		case p.From == p.To:
			return Invalid(at+".to", "must differ from from")
		case seen[[2]string{p.Provider, p.From}]:
			return Invalid(at+".from", "duplicate pair for this provider")
		}
		seen[[2]string{p.Provider, p.From}] = true
	}
	return nil
}

// RouterConfigPatch is the PATCH /api/workspaces/{id}/router body. ThresholdBp 0 resets the threshold to the
// mode's default; ResetMap true returns to the default map (a map sent with it is ignored). Map [] stores an empty
// workspace map (no routes). An unknown key is a 422.
type RouterConfigPatch struct {
	Mode        *RouterMode  `json:"mode,omitempty"`
	ThresholdBp *int         `json:"thresholdBp,omitempty"`
	Map         *[]RoutePair `json:"map,omitempty"`
	ResetMap    *bool        `json:"resetMap,omitempty"`
}

// Apply returns c with every field the patch names replaced; callers then Validate.
func (p RouterConfigPatch) Apply(c RouterConfig) RouterConfig {
	setIf(&c.Mode, p.Mode)
	if t := p.ThresholdBp; t != nil {
		if *t == 0 {
			c.ThresholdBp = nil
		} else {
			v := *t
			c.ThresholdBp = &v
		}
	}
	switch {
	case p.ResetMap != nil && *p.ResetMap:
		c.Map = nil
	case p.Map != nil:
		c.Map = append([]RoutePair{}, *p.Map...)
	}
	return c
}

// Touched lists the fields the patch names, in struct order.
func (p RouterConfigPatch) Touched() []string {
	return fieldList([]string{"mode", "thresholdBp", "map"},
		[]bool{p.Mode != nil, p.ThresholdBp != nil, p.Map != nil || (p.ResetMap != nil && *p.ResetMap)})
}
