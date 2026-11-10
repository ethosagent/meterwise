package monitors

import (
	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// fuzzyDuplicate looks for near-identical arguments of the same tool in the last LookBack assistant
// turns of the request itself (the ring keeps hashes, not arguments), stopping at a write.
func fuzzyDuplicate(c *tollgate.Call, calls []contract.CallSummary, k contract.DuplicateToolCallConfig) (contract.MonitorEvidence, bool) {
	var turns []*tollgate.Message
	for i := len(c.Messages) - 1; i >= 0 && len(turns) <= k.LookBack; i-- {
		if c.Messages[i].Role == tollgate.RoleAssistant {
			turns = append(turns, &c.Messages[i])
		}
	}
	if len(turns) < 2 {
		return contract.MonitorEvidence{}, false
	}
	for _, tc := range turns[0].ToolCalls {
		want := tokenSet(tc.Arguments)
		matched := []int{}
		for j := 1; j < len(turns); j++ {
			if similarCall(turns[j], tc, want, k.FuzzyPct) {
				matched = append(matched, ringIndex(calls, j))
			}
			if turnWrites(turns[j]) {
				break
			}
		}
		if 1+len(matched) >= k.MaxRepeats {
			return contract.MonitorEvidence{
				MatchedCalls: append(reversed(matched), ringIndex(calls, 0)), Tool: tc.Name,
				ArgsHash: hashID(c, contract.ToolArgsHash(tc.Name, tc.Arguments)), Count: 1 + len(matched), Threshold: k.MaxRepeats,
			}, true
		}
	}
	return contract.MonitorEvidence{}, false
}

func similarCall(m *tollgate.Message, tc tollgate.ToolCall, want map[string]struct{}, pct int) bool {
	for _, p := range m.ToolCalls {
		if p.Name == tc.Name && jaccardPct(want, tokenSet(p.Arguments)) >= pct {
			return true
		}
	}
	return false
}

func turnWrites(m *tollgate.Message) bool {
	for _, tc := range m.ToolCalls {
		if contract.ClassifyTool(tc.Name) == contract.ToolWrite {
			return true
		}
	}
	return false
}

// ringIndex maps the j-th newest assistant turn of the request to its ring call index (0 when unknown).
func ringIndex(calls []contract.CallSummary, j int) int {
	if i := len(calls) - 1 - j; i >= 0 {
		return calls[i].Index
	}
	return 0
}

func reversed(s []int) []int {
	out := make([]int, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}

func tokenSet(b []byte) map[string]struct{} {
	set := map[string]struct{}{}
	for _, t := range contract.Tokens(string(b), 0) {
		set[t] = struct{}{}
	}
	return set
}

// jaccardPct is |a ∩ b| / |a ∪ b| as a whole percentage (integer arithmetic, no floats).
func jaccardPct[K comparable](a, b map[K]struct{}) int {
	if len(a) == 0 && len(b) == 0 {
		return 100
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	return inter * 100 / (len(a) + len(b) - inter)
}
