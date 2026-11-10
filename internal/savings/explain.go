package savings

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Explain limits: a target must repeat at least minRepeats times to earn a suggestion, a list holds at most
// maxSuggestions, and a name that is shown is cut at maxShown bytes.
const (
	minRepeats     = 2
	maxSuggestions = 5
	maxShown       = 120
	hashPrefix     = 8
)

// Explain turns the repetition targets of a window or a session into prompt suggestions, most repeated
// first (ties by monitor, tool, path, hash, so the order is stable). It uses fixed templates only, never
// message text (rules R7 and S11): a path is named only when one was stored, otherwise the text names
// the tool and the 8-hex prefix of the path hash. Targets of a monitor that does not repeat work, and
// targets repeated fewer than minRepeats times, produce nothing. The result is never nil.
func Explain(targets []contract.RepeatTarget) []contract.Suggestion {
	out := []contract.Suggestion{}
	for _, t := range targets {
		if t.Monitor.IsRepetition() && t.Count >= minRepeats && shown(t.Tool) != "" {
			out = append(out, suggestionOf(t))
		}
	}
	slices.SortFunc(out, func(a, b contract.Suggestion) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Monitor, b.Monitor), cmp.Compare(a.Tool, b.Tool),
			cmp.Compare(deref(a.Path), deref(b.Path)), cmp.Compare(deref(a.PathHash), deref(b.PathHash)))
	})
	return out[:min(len(out), maxSuggestions)]
}

// Primary is the one suggestion line for a session: the text of the most repeated target, nil when none.
func Primary(s []contract.Suggestion) *string {
	if len(s) == 0 {
		return nil
	}
	return &s[0].Text
}

func suggestionOf(t contract.RepeatTarget) contract.Suggestion {
	s := contract.Suggestion{Monitor: t.Monitor, Tool: shown(t.Tool), Count: t.Count}
	switch p := shown(t.Path); {
	case p != "":
		s.Path = &p
	case t.PathHash != "":
		h := strings.ToLower(t.PathHash[:min(len(t.PathHash), hashPrefix)])
		s.PathHash = &h
	}
	s.Text = textOf(s)
	return s
}

// textOf fills the fixed template of the suggestion's monitor and target kind.
func textOf(s contract.Suggestion) string {
	const reuse = "Tell the agent to reuse the first result instead of calling it again."
	const keep = "Tell the agent to keep what it needs from it in its plan."
	// Count is how many times the monitor fired, not how many reads or calls each firing saw, so the text says
	// "monitor fired" and never claims an exact repeat count.
	switch {
	case s.Monitor == contract.MonitorRepeatedRead && s.Path != nil:
		return fmt.Sprintf("%s re-read %s (monitor fired %d times). Tell the agent to keep what it needs from the file in its plan, or to read only the lines it needs.", s.Tool, *s.Path, s.Count)
	case s.Monitor == contract.MonitorRepeatedRead && s.PathHash != nil:
		return fmt.Sprintf("%s re-read the same file (hash %s; monitor fired %d times). %s", s.Tool, *s.PathHash, s.Count, keep)
	case s.Monitor == contract.MonitorRepeatedRead:
		return fmt.Sprintf("%s re-read the same file (monitor fired %d times). %s", s.Tool, s.Count, keep)
	case s.Path != nil:
		return fmt.Sprintf("%s repeated the same call on %s (monitor fired %d times). %s", s.Tool, *s.Path, s.Count, reuse)
	case s.PathHash != nil:
		return fmt.Sprintf("%s repeated the same call (hash %s; monitor fired %d times). %s", s.Tool, *s.PathHash, s.Count, reuse)
	}
	return fmt.Sprintf("%s repeated the same call (monitor fired %d times). %s", s.Tool, s.Count, reuse)
}

// shown makes a stored name safe to put in a sentence: no control characters, trimmed, at most maxShown bytes.
func shown(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, s)
	return contract.TruncateUTF8(strings.TrimSpace(s), maxShown)
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
