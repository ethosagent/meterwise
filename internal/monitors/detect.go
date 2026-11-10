package monitors

import (
	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// DetectFunc decides whether one monitor fires for the call c, given the ring of the session's
// earlier calls (newest last) and the workspace config. It is pure: no I/O, no clock. The verdict
// carries Monitor, Mode (as configured) and Evidence; the Hook fills Basis and Action.
type DetectFunc func(c *tollgate.Call, r contract.RingView, cfg contract.MonitorConfig) (contract.MonitorVerdict, bool)

// Detector is one registry entry. Stream detectors run in After over the text OnEvent collected;
// the others run in Before.
type Detector struct {
	Name   contract.MonitorName
	Stream bool
	Detect DetectFunc
}

// Registry returns the six detectors in the fixed contract.MonitorNames order.
func Registry() []Detector {
	return []Detector{
		{contract.MonitorDuplicateToolCall, false, DetectDuplicateToolCall},
		{contract.MonitorRepeatedRead, false, DetectRepeatedRead},
		{contract.MonitorNoProgress, false, DetectNoProgress},
		{contract.MonitorStepBudget, false, DetectStepBudget},
		{contract.MonitorReasoningLoop, true, DetectReasoningLoop},
		{contract.MonitorSecondGuessing, true, DetectSecondGuessing},
	}
}

func hasWrite(c contract.CallSummary) bool { return len(c.WroteHashes) > 0 }

func newVerdict(name contract.MonitorName, mode contract.MonitorMode, ev contract.MonitorEvidence) contract.MonitorVerdict {
	return contract.MonitorVerdict{Monitor: name, Mode: mode, Evidence: ev}
}

// hashID is the privacy-safe public id of a ring hash inside the call's workspace (ruling R7).
func hashID(c *tollgate.Call, h uint64) string {
	return contract.HashID(c.Tenant.ID, contract.HashValue(h))
}
