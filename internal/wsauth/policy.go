package wsauth

import (
	"github.com/ethosagent/tollgate"
)

// Settings keys the workspace settings object may hold; anything else is ignored.
const (
	SettingAllowedProviders = "allowedProviders"
	SettingAllowedModels    = "allowedModels"
	SettingCapture          = "capture"
	SettingPrivacyMode      = "privacyMode"
)

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
