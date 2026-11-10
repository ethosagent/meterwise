package wsauth_test

import (
	"encoding/json"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

func TestResolveEncodesTheMonitorConfigOnceAsAnAttr(t *testing.T) {
	def := contract.DefaultMonitorConfig()
	partial := def
	partial.StepBudget.Mode, partial.StepBudget.MaxSteps = contract.ModeBlock, 12
	badField := def // a wrong-typed maxSteps is skipped and keeps its default; the mode still applies
	badField.StepBudget.Mode = contract.ModeNote
	var fromJSON any
	if err := json.Unmarshal([]byte(`{"step_budget":{"mode":"block","maxSteps":12}}`), &fromJSON); err != nil {
		t.Fatal(err)
	}
	var wrongType any
	if err := json.Unmarshal([]byte(`{"step_budget":{"mode":"note","maxSteps":"lots"}}`), &wrongType); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		settings map[string]any
		want     contract.MonitorConfig
	}{
		{"nil settings", nil, def},
		{"missing key", map[string]any{"capture": true}, def},
		{"null", map[string]any{"monitors": nil}, def},
		{"garbage string", map[string]any{"monitors": "{not json"}, def},
		{"wrong container type", map[string]any{"monitors": []any{1, 2}}, def},
		{"decoded partial map", map[string]any{"monitors": fromJSON}, partial},
		{"typed config", map[string]any{"monitors": partial}, partial},
		{"json text", map[string]any{"monitors": `{"step_budget":{"mode":"block","maxSteps":12}}`}, partial},
		{"wrong-typed field keeps the rest", map[string]any{"monitors": wrongType}, badField},
		{"out of range resets that field", map[string]any{"monitors": map[string]any{"step_budget": map[string]any{"maxSteps": 99999}}}, def},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := wsauth.Resolve(contract.Workspace{ID: wsID, OrgID: orgID, Settings: tc.settings}).(*wsauth.Resolved)
			attr, ok := res.Tenant.Attrs[wsauth.AttrMonitors]
			if !ok {
				t.Fatalf("no %q attr", wsauth.AttrMonitors)
			}
			if attr != tc.want.Attr() {
				t.Fatalf("attr = %s\nwant   %s", attr, tc.want.Attr())
			}
			if got := contract.ParseMonitorAttr(attr); got != tc.want {
				t.Fatalf("round trip = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestAMonitorSettingsChangeShowsOnTheNextResolve(t *testing.T) {
	a := wsauth.Resolve(contract.Workspace{ID: wsID, Settings: map[string]any{}}).(*wsauth.Resolved)
	b := wsauth.Resolve(contract.Workspace{ID: wsID, Settings: map[string]any{"monitors": map[string]any{"repeated_read": map[string]any{"mode": "note"}}}}).(*wsauth.Resolved)
	if a.Tenant.Attrs[wsauth.AttrMonitors] == b.Tenant.Attrs[wsauth.AttrMonitors] {
		t.Fatal("a mode change must change the attr")
	}
}
