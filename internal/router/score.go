package router

import (
	"strings"
	"unicode"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Weights are the scorer's table (basis points of the 0-10000 simplicity score; lower is simpler, so lower routes).
// The numbers are policy, set once here and changed only with an ADR.
type Weights struct {
	Base             int // where every call starts
	ReadStep         int // the previous turn only read, listed or searched
	WriteStep        int // the previous turn changed something (write, edit, shell)
	LongOutput       int // the previous turn answered with more than LongOutputTokens
	HintInjected     int // memory put a hint into this call (Plan 09), which makes the step easier
	LongOutputTokens int64
	FirstSteps       int // calls up to and including this index are never routed
}

// DefaultWeights are the weights the hook uses.
var DefaultWeights = Weights{Base: 5000, ReadStep: -2500, WriteStep: 2000, LongOutput: 1500, HintInjected: -1000, LongOutputTokens: 1500, FirstSteps: 2}

// Input is what the scorer knows about one call, all of it already in memory.
type Input struct {
	CallIndex     int                    // 1-based index of this call in its session
	Ring          []contract.CallSummary // the session's recent calls, newest last
	LastToolError bool                   // the newest tool result looks like a failure
	Escalated     bool                   // a monitor fired on a recent routed call
	HintInjected  bool                   // memory injected a hint into this call
}

// Result is a score and, when the call must stay on the strong model whatever the score, why.
type Result struct {
	Bp   int    // 0-10000, clamped
	Held string // "", or one of contract.HeldFirstSteps, HeldEscalated, HeldErrorRetry
}

// Score computes the simplicity score of a call. It is pure: the same input gives the same result. Held reasons
// are checked in this order: the first steps of a session, an escalation, a failing tool result.
func (w Weights) Score(in Input) Result {
	bp := w.Base
	if last, ok := newest(in.Ring); ok {
		read, write := kinds(last.Tools)
		switch {
		case write:
			bp += w.WriteStep
		case read:
			bp += w.ReadStep
		}
		if last.UsageKnown && last.Output > w.LongOutputTokens {
			bp += w.LongOutput
		}
	}
	if in.HintInjected {
		bp += w.HintInjected
	}
	r := Result{Bp: min(max(bp, 0), 10000)}
	switch {
	case in.CallIndex <= w.FirstSteps:
		r.Held = contract.HeldFirstSteps
	case in.Escalated:
		r.Held = contract.HeldEscalated
	case in.LastToolError:
		r.Held = contract.HeldErrorRetry
	}
	return r
}

func newest(ring []contract.CallSummary) (contract.CallSummary, bool) {
	if len(ring) == 0 {
		return contract.CallSummary{}, false
	}
	return ring[len(ring)-1], true
}

// mechanicalWords are tool-name words of steps that only look: reading, listing and searching. contract.ClassifyTool
// knows read, view, cat, open and fetch; the router adds the listing and searching names agents use.
var mechanicalWords = map[string]bool{
	"list": true, "ls": true, "dir": true, "tree": true, "grep": true, "rg": true, "find": true, "search": true,
	"glob": true, "head": true, "tail": true, "stat": true, "lookup": true,
}

// kinds says whether a turn's tools only read (read) or at least one may change the world (write). Writes win, as in
// contract.ClassifyTool: a turn that mixes both is a write. A turn with no tool is neither.
func kinds(tools []string) (read, write bool) {
	for _, name := range tools {
		switch contract.ClassifyTool(name) {
		case contract.ToolWrite:
			return false, true
		case contract.ToolRead:
			read = true
		default:
			if hasWord(name, mechanicalWords) {
				read = true
			}
		}
	}
	return read, false
}

// hasWord reports whether any word of a tool name (split on non-alphanumerics and camelCase) is in set.
func hasWord(name string, set map[string]bool) bool {
	start, runes := 0, []rune(name)
	flush := func(end int) bool {
		return end > start && set[strings.ToLower(string(runes[start:end]))]
	}
	for i, r := range runes {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			if flush(i) {
				return true
			}
			start = i + 1
		case unicode.IsUpper(r) && i > start && unicode.IsLower(runes[i-1]):
			if flush(i) {
				return true
			}
			start = i
		}
	}
	return flush(len(runes))
}
