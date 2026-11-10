package savings_test

import (
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/savings"
)

func TestEstimate(t *testing.T) {
	basis := contract.EstBasis{PriorInputTokens: 1000, BodyDeltaBytes: 401, MedianOutputTokens: 70}
	tokens := contract.Usage{Input: 1000 + 101, Output: 70}
	cases := []struct {
		name string
		v    contract.MonitorVerdict
		u    contract.Usage
		c    contract.Confidence
	}{
		{"duplicate blocked is certain", mk(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ModeBlock, basis), tokens, contract.ConfidenceCertain},
		{"duplicate observed under block mode is would-save certain", mk(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ModeObserve, basis), tokens, contract.ConfidenceCertain},
		{"duplicate observed under observe mode is likely", mk(contract.MonitorDuplicateToolCall, contract.ModeObserve, contract.ModeObserve, basis), tokens, contract.ConfidenceLikely},
		{"duplicate noted is likely", mk(contract.MonitorDuplicateToolCall, contract.ModeNote, contract.ModeNote, basis), tokens, contract.ConfidenceLikely},
		{"duplicate block degraded to note is likely", mk(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ModeNote, basis), tokens, contract.ConfidenceLikely},
		{"repeated read blocked after N is certain", mk(contract.MonitorRepeatedRead, contract.ModeNote, contract.ModeBlock, basis), tokens, contract.ConfidenceCertain},
		{"repeated read noted is likely", mk(contract.MonitorRepeatedRead, contract.ModeNote, contract.ModeNote, basis), tokens, contract.ConfidenceLikely},
		{"no progress is always likely", mk(contract.MonitorNoProgress, contract.ModeBlock, contract.ModeBlock, basis), tokens, contract.ConfidenceLikely},
		{"step budget is nothing", mk(contract.MonitorStepBudget, contract.ModeNote, contract.ModeNote, basis), contract.Usage{}, contract.ConfidenceNone},
		{"reasoning loop is nothing", mk(contract.MonitorReasoningLoop, contract.ModeNote, contract.ModeNote, basis), contract.Usage{}, contract.ConfidenceNone},
		{"second guessing is nothing", mk(contract.MonitorSecondGuessing, contract.ModeObserve, contract.ModeObserve, basis), contract.Usage{}, contract.ConfidenceNone},
		{"zero basis is zero tokens", mk(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ModeBlock, contract.EstBasis{}), contract.Usage{}, contract.ConfidenceCertain},
		{"negative growth never subtracts", mk(contract.MonitorNoProgress, contract.ModeNote, contract.ModeNote, contract.EstBasis{PriorInputTokens: 10, BodyDeltaBytes: -900}), contract.Usage{Input: 10}, contract.ConfidenceLikely},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, c := savings.Estimate(tc.v)
			if u != tc.u || c != tc.c {
				t.Errorf("Estimate = %+v %s, want %+v %s", u, c, tc.u, tc.c)
			}
		})
	}
}

func mk(m contract.MonitorName, mode, action contract.MonitorMode, b contract.EstBasis) contract.MonitorVerdict {
	return contract.MonitorVerdict{Monitor: m, Mode: mode, Action: action, Basis: b}
}
