package monitors

import (
	"cmp"
	"slices"

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
	sets := newTurnSets(turns)
	for _, tc := range turns[0].ToolCalls {
		want := tokenSet(tc.Arguments)
		matched := []int{}
		for j := 1; j < len(turns); j++ {
			if sets.similar(j, tc.Name, want, k.FuzzyPct) {
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

// turnSets holds the token sets of the tool-call arguments of the request's recent turns, each built the
// first time a comparison needs it and then shared by every tool call of the newest turn.
type turnSets struct {
	turns []*tollgate.Message
	sets  [][][]string // [turn][call]; nil until built
}

func newTurnSets(turns []*tollgate.Message) *turnSets {
	return &turnSets{turns: turns, sets: make([][][]string, len(turns))}
}

// similar reports whether turn j calls tool name with arguments at least pct percent like want.
func (t *turnSets) similar(j int, name string, want []string, pct int) bool {
	calls := t.turns[j].ToolCalls
	for i, p := range calls {
		if p.Name != name {
			continue
		}
		if t.sets[j] == nil {
			t.sets[j] = make([][]string, len(calls))
		}
		if t.sets[j][i] == nil {
			t.sets[j][i] = tokenSet(p.Arguments)
			if t.sets[j][i] == nil {
				t.sets[j][i] = []string{} // an empty set, built
			}
		}
		if jaccardAtLeast(want, t.sets[j][i], pct) {
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

// tokenSet is the distinct lowercase letter/digit tokens of b, sorted.
func tokenSet(b []byte) []string {
	t := contract.Tokens(string(b), 0)
	slices.Sort(t)
	return slices.Compact(t)
}

// jaccardAtLeast is jaccardPct(a, b) >= pct, rejecting without a walk when the sizes alone rule it out:
// the intersection is at most the smaller set and the union at least the larger.
func jaccardAtLeast(a, b []string, pct int) bool {
	lo, hi := min(len(a), len(b)), max(len(a), len(b))
	if hi > 0 && lo*100/hi < pct {
		return false
	}
	return jaccardPct(a, b) >= pct
}

// jaccardPct is |a ∩ b| / |a ∪ b| as a whole percentage (integer arithmetic, no floats) of two sorted
// sets without repeats, by one merge walk.
func jaccardPct[K cmp.Ordered](a, b []K) int {
	if len(a) == 0 && len(b) == 0 {
		return 100
	}
	inter := 0
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch c := cmp.Compare(a[i], b[j]); {
		case c == 0:
			inter++
			i++
			j++
		case c < 0:
			i++
		default:
			j++
		}
	}
	return inter * 100 / (len(a) + len(b) - inter)
}

// sortedSet is h as a set sorted ascending without repeats: h itself when the ring already stored it so
// (history does), else a sorted copy.
func sortedSet(h []uint64) []uint64 {
	for i := 1; i < len(h); i++ {
		if h[i-1] >= h[i] {
			return slices.Compact(slices.Sorted(slices.Values(h)))
		}
	}
	return h
}
