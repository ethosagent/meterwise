package monitors

import (
	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// DetectReasoningLoop fires when the response's reasoning repeats a run of at least ShingleTokens tokens,
// either inside itself or against the opening of one of the session's last three turns (the ring keeps a
// bounded shingle prefix per turn). It counts reasoning text only: an answer that shows code before and
// after a fix repeats itself on purpose (M3). It reads what OnEvent collected, so it only sees streams.
func DetectReasoningLoop(c *tollgate.Call, r contract.RingView, cfg contract.MonitorConfig) (contract.MonitorVerdict, bool) {
	k := cfg.ReasoningLoop
	st := stateOf(c)
	if st == nil {
		return contract.MonitorVerdict{}, false
	}
	_, reasoning, _ := st.snapshot()
	win := contract.ShinglesOf(contract.Tokens(reasoning, maxStreamTokens), maxWindows)
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

// reconsider is the fixed dictionary of second-guessing phrases, as token sequences. Every phrase has at
// least two words: single words such as "wait", "actually" or "hmm" are everyday wording (M3). Evidence
// holds only these phrases, never the model's own words.
var reconsider = [][]string{
	{"on", "second", "thought"}, {"let", "me", "reconsider"}, {"let", "me", "rethink"}, {"i", "made", "a", "mistake"},
	{"i", "was", "wrong"}, {"scratch", "that"}, {"on", "reflection"}, {"hold", "on"}, {"no", "wait"},
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

// DetectSecondGuessing fires when the response (answer text and reasoning) holds at least MinPhrases
// reconsideration phrases and its first tool call is a reversal: the very call made two calls ago (same
// tool and same arguments hash) after a different one just before (A-B-A). The same tool with other
// arguments (Read, Bash, Read of another file) is everyday work, not a reversal (M3).
func DetectSecondGuessing(c *tollgate.Call, r contract.RingView, cfg contract.MonitorConfig) (contract.MonitorVerdict, bool) {
	k := cfg.SecondGuessing
	st := stateOf(c)
	if st == nil {
		return contract.MonitorVerdict{}, false
	}
	text, reasoning, tools := st.snapshot()
	calls := r.Calls()
	n := len(calls)
	if len(tools) == 0 || n < 2 || !reversal(contract.ToolArgsHash(tools[0].name, tools[0].args), calls[n-2], calls[n-1]) {
		return contract.MonitorVerdict{}, false
	}
	total, phrases := countPhrases(contract.Tokens(text+" "+reasoning, maxStreamTokens))
	if total < k.MinPhrases {
		return contract.MonitorVerdict{}, false
	}
	return newVerdict(contract.MonitorSecondGuessing, k.Mode, contract.MonitorEvidence{
		MatchedCalls: []int{calls[n-2].Index, calls[n-1].Index, c.Session.Calls}, Tool: tools[0].name,
		Phrases: phrases, Count: total, Threshold: k.MinPhrases,
	}), true
}

// reversal reports whether call h goes back to a's first call after b's first call left it (A-B-A).
func reversal(h uint64, a, b contract.CallSummary) bool {
	if len(a.ArgsHashes) == 0 || a.ArgsHashes[0] != h {
		return false
	}
	return len(b.ArgsHashes) == 0 || b.ArgsHashes[0] != h
}
