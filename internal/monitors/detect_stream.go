package monitors

import (
	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// DetectReasoningLoop fires when the response repeats a run of at least ShingleTokens tokens, either
// inside itself or against the opening of one of the session's last three turns (the ring keeps a
// bounded shingle prefix per turn). It reads the text OnEvent collected, so it only sees streams.
func DetectReasoningLoop(c *tollgate.Call, r contract.RingView, cfg contract.MonitorConfig) (contract.MonitorVerdict, bool) {
	k := cfg.ReasoningLoop
	st := stateOf(c)
	if st == nil {
		return contract.MonitorVerdict{}, false
	}
	text, _ := st.snapshot()
	win := contract.ShinglesOf(contract.Tokens(text, maxStreamTokens), maxWindows)
	need := max(1, k.ShingleTokens-contract.ShingleSize+1)
	run, at := selfRun(win)
	var matched []int
	calls := r.Calls()
	for _, prev := range calls[max(0, len(calls)-3):] {
		if rr, a := setRun(win, hashSet(prev.Shingles)); rr > run {
			run, at, matched = rr, a, []int{prev.Index}
		}
	}
	if run < need {
		return contract.MonitorVerdict{}, false
	}
	return newVerdict(contract.MonitorReasoningLoop, k.Mode, contract.MonitorEvidence{
		MatchedCalls: append(matched, c.Session.Calls), Hash: hashID(c, win[at]),
		Count: run + contract.ShingleSize - 1, Threshold: k.ShingleTokens,
	}), true
}

// selfRun is the longest run of consecutive windows that each repeat the window one step after the
// previous match, and where that run starts.
func selfRun(win []uint64) (best, at int) {
	seen := map[uint64]int{}
	run, prevQ := 0, -1
	for i, w := range win {
		q, ok := seen[w]
		switch {
		case !ok:
			run = 0
		case run > 0 && q == prevQ+1:
			run++
		default:
			run = 1
		}
		if run > best {
			best, at = run, i-run+1
		}
		prevQ = q
		seen[w] = i
	}
	return best, at
}

// setRun is the longest run of consecutive windows that all appear in set, and where it starts.
func setRun(win []uint64, set map[uint64]struct{}) (best, at int) {
	run := 0
	for i, w := range win {
		if _, ok := set[w]; !ok {
			run = 0
			continue
		}
		run++
		if run > best {
			best, at = run, i-run+1
		}
	}
	return best, at
}

// reconsider is the fixed dictionary of second-guessing phrases, as token sequences. Evidence holds
// only these phrases, never the model's own words.
var reconsider = [][]string{
	{"on", "second", "thought"}, {"let", "me", "reconsider"}, {"let", "me", "rethink"}, {"i", "made", "a", "mistake"},
	{"i", "was", "wrong"}, {"scratch", "that"}, {"on", "reflection"}, {"hold", "on"}, {"no", "wait"},
	{"wait"}, {"actually"}, {"hmm"},
}

// countPhrases counts dictionary phrases in tokens (longest match first, no overlap) and names the
// distinct ones in dictionary order.
func countPhrases(tokens []string) (total int, distinct []string) {
	hit := make([]bool, len(reconsider))
	for i := 0; i < len(tokens); {
		j := matchAt(tokens, i)
		if j < 0 {
			i++
			continue
		}
		total++
		hit[j] = true
		i += len(reconsider[j])
	}
	for j, h := range hit {
		if h {
			distinct = append(distinct, joinWords(reconsider[j]))
		}
	}
	return total, distinct
}

func matchAt(tokens []string, i int) int {
	for j, p := range reconsider {
		if i+len(p) > len(tokens) {
			continue
		}
		ok := true
		for k, w := range p {
			if tokens[i+k] != w {
				ok = false
				break
			}
		}
		if ok {
			return j
		}
	}
	return -1
}

func joinWords(w []string) string {
	out := w[0]
	for _, s := range w[1:] {
		out += " " + s
	}
	return out
}

// DetectSecondGuessing fires when the response holds at least MinPhrases reconsideration phrases and
// its first tool is a reversal: the same tool as two calls ago but not the one just before (A-B-A).
func DetectSecondGuessing(c *tollgate.Call, r contract.RingView, cfg contract.MonitorConfig) (contract.MonitorVerdict, bool) {
	k := cfg.SecondGuessing
	st := stateOf(c)
	if st == nil {
		return contract.MonitorVerdict{}, false
	}
	text, tools := st.snapshot()
	total, phrases := countPhrases(contract.Tokens(text, maxStreamTokens))
	calls := r.Calls()
	n := len(calls)
	if total < k.MinPhrases || len(tools) == 0 || n < 2 || toolAt(calls[n-2], 0) != tools[0] || toolAt(calls[n-1], 0) == tools[0] {
		return contract.MonitorVerdict{}, false
	}
	return newVerdict(contract.MonitorSecondGuessing, k.Mode, contract.MonitorEvidence{
		MatchedCalls: []int{calls[n-2].Index, calls[n-1].Index, c.Session.Calls}, Tool: tools[0],
		Phrases: phrases, Count: total, Threshold: k.MinPhrases,
	}), true
}
