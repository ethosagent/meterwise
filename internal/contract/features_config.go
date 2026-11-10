package contract

import (
	"encoding/json"
	"errors"
)

// The per-workspace settings of the three savers live at workspaces.settings.{trim,router,memory}, next to
// "monitors". Each type follows the monitor pattern: Default*, *From (bad fields reset to defaults), Attr (canonical
// JSON for Tenant.Attrs), Parse*Attr, Validate (422 with a field path) and a pointer-field *Patch with Apply and
// Touched. This file holds what they share.

// Setting keys under workspaces.settings; the keys PATCH /api/workspaces/{id} must keep and
// SetWorkspaceSetting accepts.
const (
	SettingMonitors = "monitors"
	SettingTrim     = "trim"
	SettingRouter   = "router"
	SettingMemory   = "memory"
)

// SaverSettingKeys are the three keys SetWorkspaceSetting may write, in a fixed order.
func SaverSettingKeys() []string { return []string{SettingTrim, SettingRouter, SettingMemory} }

// settingFrom decodes whatever a settings key held: a T, *T, JSON text or bytes, or a decoded map. Missing,
// malformed and partial input is filled with defaults (def), then fix resets any invalid field.
func settingFrom[T any](v any, def func() T, fix func(*T)) T {
	var raw []byte
	switch t := v.(type) {
	case nil:
		return def()
	case T:
		fix(&t)
		return t
	case *T:
		if t == nil {
			return def()
		}
		c := *t
		fix(&c)
		return c
	case string:
		raw = []byte(t)
	case []byte:
		raw = t
	case json.RawMessage:
		raw = t
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return def()
		}
		raw = b
	}
	cfg := def()
	// A wrong-typed field is skipped by Unmarshal and the rest still decode; only a syntax error aborts.
	if err := json.Unmarshal(raw, &cfg); err != nil {
		var te *json.UnmarshalTypeError
		if !errors.As(err, &te) {
			return def()
		}
	}
	fix(&cfg)
	return cfg
}

// attrOf is the canonical JSON of a settings value (struct field order is fixed).
func attrOf(v any) string {
	b, err := json.Marshal(v)
	if err != nil { // unreachable: strings, ints, bools, maps of bools
		return ""
	}
	return string(b)
}

func intInRange(p *int, def, lo, hi int) {
	if *p < lo || *p > hi {
		*p = def
	}
}

func outside(v, lo, hi int) bool { return v < lo || v > hi }

// fieldList names, in order, the patch fields that are set.
func fieldList(names []string, set []bool) []string {
	out := []string{}
	for i, n := range names {
		if set[i] {
			out = append(out, n)
		}
	}
	return out
}

// cleanStrings keeps the entries that are 1..maxLen bytes, at most max of them, in order; never nil.
func cleanStrings(in []string, maxN, maxLen int) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" && len(s) <= maxLen && len(out) < maxN {
			out = append(out, s)
		}
	}
	return out
}
