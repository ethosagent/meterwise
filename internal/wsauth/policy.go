package wsauth

import (
	"encoding/json"
	"math"

	"github.com/ethosagent/tollgate"
)

// Settings keys the workspace settings object may hold; anything else is ignored.
const (
	SettingAllowedProviders = "allowedProviders"
	SettingAllowedModels    = "allowedModels"
	SettingCapture          = "capture"
	SettingPrivacyMode      = "privacyMode"
	SettingMaxConcurrent    = "maxConcurrent"
)

// MaxConcurrentCap is the largest per-workspace concurrency override honoured. A larger value is
// clamped to it, so a settings blob cannot ask for an unbounded number of slots.
const MaxConcurrentCap = 1024

// MaxConcurrentOf reads settings.maxConcurrent: a whole number of at least 1 (JSON numbers arrive
// as float64 or json.Number, tests may pass int). It returns 0 for a missing, mistyped,
// fractional or non-positive value, meaning "use the default", and clamps to MaxConcurrentCap.
func MaxConcurrentOf(settings map[string]any) int {
	var n int64
	switch v := settings[SettingMaxConcurrent].(type) {
	case int:
		n = int64(v)
	case int64:
		n = v
	case float64:
		if v != math.Trunc(v) || v < 1 { // also NaN
			return 0
		}
		n = int64(min(v, MaxConcurrentCap))
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0
		}
		n = i
	default:
		return 0
	}
	if n < 1 {
		return 0
	}
	return int(min(n, MaxConcurrentCap))
}

// PolicyOf turns workspace settings into the tollgate per-tenant policy. A missing or mistyped key
// takes tollgate's default (all providers, all models, no capture, inherit privacy mode), so a bad
// settings blob can only widen nothing and narrow nothing by accident. It returns nil when the
// settings set nothing.
func PolicyOf(settings map[string]any) *tollgate.Policy {
	p := tollgate.Policy{
		AllowedProviders: stringList(settings[SettingAllowedProviders]),
		AllowedModels:    stringList(settings[SettingAllowedModels]),
	}
	if b, ok := settings[SettingCapture].(bool); ok {
		p.Capture = b
	}
	if b, ok := settings[SettingPrivacyMode].(bool); ok {
		p.PrivacyMode = &b
	}
	if len(p.AllowedProviders) == 0 && len(p.AllowedModels) == 0 && !p.Capture && p.PrivacyMode == nil {
		return nil
	}
	return &p
}

func stringList(v any) []string {
	list, ok := v.([]any)
	if !ok {
		if s, ok := v.([]string); ok {
			return s
		}
		return nil
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		if s, ok := e.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
