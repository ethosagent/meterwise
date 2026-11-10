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

// writesBefore reports whether turn s runs a write before its tool at position ti. The pending turn's own
// write changes what its later calls see, so it breaks a repeat just as an earlier turn's write does (M2).
func writesBefore(s contract.CallSummary, ti int) bool {
	for _, name := range s.Tools[:max(0, min(ti, len(s.Tools)))] {
		if contract.ClassifyTool(name) == contract.ToolWrite {
			return true
		}
	}
	return false
}

// readAt is the tool position of the r-th read of turn s, -1 when there is none.
func readAt(s contract.CallSummary, r int) int {
	for i, name := range s.Tools {
		if contract.ClassifyTool(name) == contract.ToolRead {
			if r == 0 {
				return i
			}
			r--
		}
	}
	return -1
}

func newVerdict(name contract.MonitorName, mode contract.MonitorMode, ev contract.MonitorEvidence) contract.MonitorVerdict {
	return contract.MonitorVerdict{Monitor: name, Mode: mode, Evidence: ev}
}

// hashID is the privacy-safe public id of a ring hash inside the call's workspace (ruling R7).
func hashID(c *tollgate.Call, h uint64) string {
	return contract.HashOf(c.Tenant.ID, h)
}
