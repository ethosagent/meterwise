package monitors

import (
	"slices"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// DetectFunc decides whether one monitor fires for the call c, given the ring of the session's
// earlier calls (newest last) and the workspace config. It is pure: no I/O, no clock. The verdict
// carries Monitor, Mode (as configured) and Evidence; the Hook fills Basis and Action.
type DetectFunc func(c *tollgate.Call, r contract.RingView, cfg contract.MonitorConfig) (contract.MonitorVerdict, bool)

// Detector is one registry entry. Stream detectors run in After over the text OnEvent collected;
// the others run in Before. CanBlock says the monitor's block mode may answer the call itself; every other
// monitor's block mode acts as note. BlockAfter, when set, is the verdict count from which the monitor's
// note mode blocks too (0 or nil: never).
type Detector struct {
	Name       contract.MonitorName
	Stream     bool
	CanBlock   bool
	BlockAfter func(cfg contract.MonitorConfig) int
	Detect     DetectFunc
}

// Registry returns the six detectors in the fixed contract.MonitorNames order.
func Registry() []Detector {
	return []Detector{
		{Name: contract.MonitorDuplicateToolCall, CanBlock: true, Detect: DetectDuplicateToolCall},
		{Name: contract.MonitorRepeatedRead, CanBlock: true, Detect: DetectRepeatedRead,
			BlockAfter: func(cfg contract.MonitorConfig) int { return cfg.RepeatedRead.BlockAfter }},
		{Name: contract.MonitorNoProgress, Detect: DetectNoProgress},
		{Name: contract.MonitorStepBudget, Detect: DetectStepBudget},
		{Name: contract.MonitorReasoningLoop, Stream: true, Detect: DetectReasoningLoop},
		{Name: contract.MonitorSecondGuessing, Stream: true, Detect: DetectSecondGuessing},
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

// repeatScan finds the first hash of the pending turn (the newest call) that its earlier calls repeat enough
// to fire: need occurrences in all, counting the pending one, among the lookBack calls before it and with no
// write in between. own picks the hashes a call holds (arguments or file reads) and pos the tool position of
// the i-th of them in that call. It returns that hash's index in the pending turn and the matching earlier
// call indexes, oldest first. The write rule lives here, once (M2): the pending turn's own write breaks a
// repeat as an earlier turn's does, and an earlier turn's write lies between it and the pending call.
func repeatScan(calls []contract.CallSummary, lookBack, need int, own func(contract.CallSummary) []uint64, pos func(contract.CallSummary, int) int) (int, []int, bool) {
	n := len(calls)
	cur := calls[n-1]
	start := max(0, n-1-lookBack)
	for i, h := range own(cur) {
		if writesBefore(cur, pos(cur, i)) {
			break // this turn wrote first: this hash and every later one sees a new state
		}
		var matched []int
		for j := n - 2; j >= start; j-- {
			if hasWrite(calls[j]) {
				break
			}
			if slices.Contains(own(calls[j]), h) {
				matched = append(matched, calls[j].Index)
			}
		}
		if 1+len(matched) >= need {
			slices.Reverse(matched)
			return i, matched, true
		}
	}
	return 0, nil, false
}

func newVerdict(name contract.MonitorName, mode contract.MonitorMode, ev contract.MonitorEvidence) contract.MonitorVerdict {
	return contract.MonitorVerdict{Monitor: name, Mode: mode, Evidence: ev}
}

// hashID is the privacy-safe public id of a ring hash inside the call's workspace (ruling R7).
func hashID(c *tollgate.Call, h uint64) string {
	return contract.HashOf(c.Tenant.ID, h)
}
