package wsauth_test

import (
	"reflect"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

// Resolve puts the three saver settings on the tenant as canonical JSON attrs, defaults filled in, so the hooks
// decode them from the cached view and never touch the store.
func TestResolveEncodesTheSaverConfigsAsAttrs(t *testing.T) {
	def := contract.DefaultTrimConfig()
	res := wsauth.Resolve(contract.Workspace{ID: wsID}).(*wsauth.Resolved)
	if got := contract.ParseTrimAttr(res.Tenant.Attrs[wsauth.AttrTrim]); !reflect.DeepEqual(got, def) {
		t.Errorf("default trim attr = %+v", got)
	}
	if got := contract.ParseRouterAttr(res.Tenant.Attrs[wsauth.AttrRouter]); !reflect.DeepEqual(got, contract.DefaultRouterConfig()) {
		t.Errorf("default router attr = %+v", got)
	}
	if got := contract.ParseMemoryAttr(res.Tenant.Attrs[wsauth.AttrMemory]); got != contract.DefaultMemoryConfig() {
		t.Errorf("default memory attr = %+v", got)
	}
	set := contract.Workspace{ID: wsID, Settings: map[string]any{
		"trim":   map[string]any{"mode": "lossless", "maxFoldPct": 50},
		"router": `{"mode":"conservative","thresholdBp":3000}`,
		"memory": map[string]any{"mode": "hint", "maxHintsPerSession": 2},
	}}
	res = wsauth.Resolve(set).(*wsauth.Resolved)
	if got := contract.ParseTrimAttr(res.Tenant.Attrs[wsauth.AttrTrim]); got.Mode != contract.TrimModeLossless || got.MaxFoldPct != 50 || got.MinGainPct != 2 {
		t.Errorf("trim attr = %+v", got)
	}
	if got := contract.ParseRouterAttr(res.Tenant.Attrs[wsauth.AttrRouter]); got.Mode != contract.RouterModeConservative || got.ThresholdBp == nil || *got.ThresholdBp != 3000 {
		t.Errorf("router attr = %+v", got)
	}
	if got := contract.ParseMemoryAttr(res.Tenant.Attrs[wsauth.AttrMemory]); got.Mode != contract.MemoryModeHint || got.MaxHintsPerSession != 2 {
		t.Errorf("memory attr = %+v", got)
	}
	// garbage falls back to the defaults field by field
	bad := wsauth.Resolve(contract.Workspace{ID: wsID, Settings: map[string]any{"trim": "{nope", "router": []any{1}}}).(*wsauth.Resolved)
	if got := contract.ParseTrimAttr(bad.Tenant.Attrs[wsauth.AttrTrim]); !reflect.DeepEqual(got, def) {
		t.Errorf("garbage trim attr = %+v", got)
	}
}
