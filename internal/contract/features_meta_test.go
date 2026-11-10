package contract_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

func TestTrimTokens(t *testing.T) {
	tests := []struct {
		name               string
		delta, sent, input int64
		want               int64
	}{
		{"exact", 1000, 4000, 1000, 250},
		{"rounds up", 1001, 4000, 1000, 251},
		{"rounds up a fraction of one", 1, 4000, 1000, 1},
		{"measured ratio, not 4 bytes per token", 600, 3000, 1500, 300},
		{"input zero falls back to a quarter", 10, 4000, 0, 3},
		{"input negative falls back", 8, 4000, -5, 2},
		{"input zero exact quarter", 400, 4000, 0, 100},
		{"sent zero falls back", 9, 0, 1000, 3},
		{"sent negative falls back", 9, -1, 1000, 3},
		{"delta zero saves nothing", 0, 4000, 1000, 0},
		{"delta negative saves nothing", -50, 4000, 1000, 0},
		{"large operands do not wrap", 1 << 40, 1 << 41, 1 << 40, 1 << 39},
		{"quotient beyond int64 saturates", math.MaxInt64, 1, math.MaxInt64, math.MaxInt64},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := contract.TrimTokens(tc.delta, tc.sent, tc.input); got != tc.want {
				t.Fatalf("TrimTokens(%d, %d, %d) = %d, want %d", tc.delta, tc.sent, tc.input, got, tc.want)
			}
		})
	}
}

func TestModelMatches(t *testing.T) {
	tests := []struct {
		name, model string
		want        bool
	}{
		{"claude-sonnet-4", "claude-sonnet-4", true},
		{"claude-sonnet-4", "claude-sonnet-4-20250514", true},
		{"claude-sonnet-4", "claude-sonnet-4-latest", true},
		{"gpt-4o", "gpt-4o-2024-07-18", true},
		{"gpt-4o", "gpt-4o:ft-acme", true},
		{"gpt-4o", "gpt-4o@v2", true},
		{"gpt-4o", "gpt-4o-mini", false},
		{"claude-opus-4", "claude-opus-4-5", false},
		{"claude-opus-4", "claude-opus-4-5-20251101", false},
		{"gpt-4o", "gpt-4", false},
		{"gpt-4o", "gpt-4o-2024-7-18", false},
		{"gpt-4o", "gpt-4o-2024071", false},
		{"gpt-4o", "gpt-4o-20240718x", false},
		{"gpt-4o", "gpt-4o20240718", false},
		{"gpt-4o", "", false},
		{"", "", true},
	}
	for _, tc := range tests {
		if got := contract.ModelMatches(tc.name, tc.model); got != tc.want {
			t.Errorf("ModelMatches(%q, %q) = %v, want %v", tc.name, tc.model, got, tc.want)
		}
	}
}

func TestCallVerdictsSnapshotCopiesWithoutSealing(t *testing.T) {
	var c contract.CallVerdicts
	v := contract.MonitorVerdict{Monitor: contract.MonitorStepBudget, Mode: contract.ModeObserve, Action: contract.ModeObserve}
	if !c.Add(v) {
		t.Fatal("first add refused")
	}
	snap := c.Snapshot()
	if len(snap) != 1 || snap[0].Monitor != contract.MonitorStepBudget {
		t.Fatalf("snapshot = %+v", snap)
	}
	snap[0].Monitor = "mutated"
	if got := c.Snapshot(); got[0].Monitor != contract.MonitorStepBudget {
		t.Fatal("Snapshot must return a copy")
	}
	if !c.Add(contract.MonitorVerdict{Monitor: contract.MonitorNoProgress}) {
		t.Fatal("Snapshot sealed the collection")
	}
	if got, _ := c.Seal(); len(got) != 2 {
		t.Fatalf("sealed list = %+v", got)
	}
	if got := c.Snapshot(); len(got) != 2 {
		t.Fatalf("snapshot after seal = %+v", got)
	}
	var empty contract.CallVerdicts
	if empty.Snapshot() != nil && len(empty.Snapshot()) != 0 {
		t.Fatal("an empty collection has an empty snapshot")
	}
}

func TestTrimStagesAndCauseOrderAreFixed(t *testing.T) {
	if got := contract.TrimStages(); !reflect.DeepEqual(got, []contract.TrimStage{"repeat_collapse", "log_window", "dup_lines", "json_table", "schema_trim"}) {
		t.Fatalf("stages = %v", got)
	}
	causes := contract.AllSavingsCauses()
	want := []contract.SavingsCause{"duplicate_tool_call", "repeated_read", "no_progress", "step_budget", "reasoning_loop", "second_guessing", "trim", "router", "memory"}
	if !reflect.DeepEqual(causes, want) {
		t.Fatalf("causes = %v", causes)
	}
	kinds := map[contract.SavingsCause]contract.CauseKind{"repeated_read": "monitor", "trim": "trim", "router": "router", "memory": "memory"}
	for c, k := range kinds {
		if c.Kind() != k {
			t.Errorf("%s kind = %s, want %s", c, c.Kind(), k)
		}
	}
	if m, ok := contract.SavingsCause("repeated_read").Monitor(); !ok || m != contract.MonitorRepeatedRead {
		t.Errorf("Monitor() = %v, %v", m, ok)
	}
	if _, ok := contract.CauseTrim.Monitor(); ok {
		t.Error("trim is not a monitor")
	}
	if got := contract.StepEventKinds(); !reflect.DeepEqual(got, []contract.StepEventKind{"monitor_fire", "trim", "route", "memory_hint"}) {
		t.Fatalf("step event kinds = %v", got)
	}
}

func TestSessionTags(t *testing.T) {
	for _, ok := range []string{"", "trimmed", "routed", "remembered"} {
		if !contract.ValidSessionTag(ok) {
			t.Errorf("%q must be valid", ok)
		}
	}
	for _, bad := range []string{"Trimmed", "memory", "x"} {
		if contract.ValidSessionTag(bad) {
			t.Errorf("%q must be invalid", bad)
		}
	}
}
