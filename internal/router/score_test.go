package router_test

import (
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/router"
)

func turn(idx int, out int64, tools ...string) contract.CallSummary {
	return contract.CallSummary{Index: idx, Tools: tools, Output: out, UsageKnown: out > 0}
}

// The scorer on fixture rings: the table is the specification (09-11-tasks.md weights).
func TestScoreTable(t *testing.T) {
	w := router.DefaultWeights
	cases := []struct {
		name     string
		in       router.Input
		wantBp   int
		wantHeld string
	}{
		{"a fresh session with no history is held for its first steps", router.Input{CallIndex: 1}, 5000, contract.HeldFirstSteps},
		{"the second call is still a first step", router.Input{CallIndex: 2, Ring: []contract.CallSummary{turn(1, 0, "read_file")}}, 2500, contract.HeldFirstSteps},
		{"read-heavy history scores low", router.Input{CallIndex: 5, Ring: []contract.CallSummary{turn(3, 0, "read_file"), turn(4, 0, "list_dir")}}, 2500, ""},
		{"grep is a mechanical step", router.Input{CallIndex: 4, Ring: []contract.CallSummary{turn(3, 0, "grep")}}, 2500, ""},
		{"camelCase listing tool", router.Input{CallIndex: 4, Ring: []contract.CallSummary{turn(3, 0, "listDirectory")}}, 2500, ""},
		{"a write scores high", router.Input{CallIndex: 4, Ring: []contract.CallSummary{turn(3, 0, "edit_file")}}, 7000, ""},
		{"a shell command is a write", router.Input{CallIndex: 4, Ring: []contract.CallSummary{turn(3, 0, "bash")}}, 7000, ""},
		{"a mixed turn is a write", router.Input{CallIndex: 4, Ring: []contract.CallSummary{turn(3, 0, "read_file", "write_file")}}, 7000, ""},
		{"an unknown tool adds nothing", router.Input{CallIndex: 4, Ring: []contract.CallSummary{turn(3, 0, "frobnicate")}}, 5000, ""},
		{"a long previous answer raises the score", router.Input{CallIndex: 4, Ring: []contract.CallSummary{turn(3, 1501, "read_file")}}, 4000, ""},
		{"exactly 1500 output tokens is not long", router.Input{CallIndex: 4, Ring: []contract.CallSummary{turn(3, 1500, "read_file")}}, 2500, ""},
		{"unknown usage never counts as long", router.Input{CallIndex: 4, Ring: []contract.CallSummary{{Index: 3, Tools: []string{"read_file"}, Output: 9999}}}, 2500, ""},
		{"a memory hint lowers it", router.Input{CallIndex: 4, HintInjected: true, Ring: []contract.CallSummary{turn(3, 0, "frobnicate")}}, 4000, ""},
		{"a failing tool result holds the call", router.Input{CallIndex: 4, LastToolError: true, Ring: []contract.CallSummary{turn(3, 0, "read_file")}}, 2500, contract.HeldErrorRetry},
		{"after a monitor fired the call is forced strong", router.Input{CallIndex: 4, Escalated: true, Ring: []contract.CallSummary{turn(3, 0, "read_file")}}, 2500, contract.HeldEscalated},
		{"first steps beat an escalation", router.Input{CallIndex: 2, Escalated: true, LastToolError: true}, 5000, contract.HeldFirstSteps},
		{"an escalation beats an error", router.Input{CallIndex: 4, Escalated: true, LastToolError: true}, 5000, contract.HeldEscalated},
		{"clamped at the top", router.Input{CallIndex: 4, Ring: []contract.CallSummary{turn(3, 9000, "bash")}}, 8500, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := w.Score(tc.in)
			if got.Bp != tc.wantBp || got.Held != tc.wantHeld {
				t.Errorf("Score = %+v, want bp %d held %q", got, tc.wantBp, tc.wantHeld)
			}
		})
	}
}

func TestScoreClampsAndIsPure(t *testing.T) {
	w := router.Weights{Base: 100, ReadStep: -5000, WriteStep: 99999, FirstSteps: 0}
	if got := w.Score(router.Input{CallIndex: 3, Ring: []contract.CallSummary{turn(2, 0, "read_file")}}); got.Bp != 0 {
		t.Errorf("below zero must clamp to 0: %+v", got)
	}
	if got := w.Score(router.Input{CallIndex: 3, Ring: []contract.CallSummary{turn(2, 0, "write_file")}}); got.Bp != 10000 {
		t.Errorf("above 10000 must clamp: %+v", got)
	}
	in := router.Input{CallIndex: 5, Ring: []contract.CallSummary{turn(4, 0, "read_file")}}
	if a, b := router.DefaultWeights.Score(in), router.DefaultWeights.Score(in); a != b {
		t.Errorf("the same input must score the same: %+v vs %+v", a, b)
	}
}

func TestAllowsIsTheCapabilityGuard(t *testing.T) {
	full := contract.ModelCaps{MaxTools: 32, JSONSchema: true, Vision: true, Reasoning: true}
	cases := []struct {
		name string
		caps contract.ModelCaps
		f    contract.RequestFeatures
		want bool
	}{
		{"plain request, any caps", contract.ModelCaps{}, contract.RequestFeatures{}, true},
		{"tools up to the max", full, contract.RequestFeatures{Tools: 32}, true},
		{"one tool too many", full, contract.RequestFeatures{Tools: 33}, false},
		{"max_tools 0 refuses any tool", contract.ModelCaps{MaxTools: 0, JSONSchema: true, Vision: true, Reasoning: true}, contract.RequestFeatures{Tools: 1}, false},
		{"json schema needs support", contract.ModelCaps{MaxTools: 32}, contract.RequestFeatures{JSONSchema: true}, false},
		{"vision needs support", contract.ModelCaps{MaxTools: 32}, contract.RequestFeatures{Vision: true}, false},
		{"reasoning needs support", contract.ModelCaps{MaxTools: 32}, contract.RequestFeatures{Reasoning: true}, false},
		{"everything supported", full, contract.RequestFeatures{Tools: 3, JSONSchema: true, Vision: true, Reasoning: true}, true},
	}
	for _, tc := range cases {
		if got := router.Allows(tc.caps, tc.f); got != tc.want {
			t.Errorf("%s: Allows = %v, want %v", tc.name, got, tc.want)
		}
	}
}
