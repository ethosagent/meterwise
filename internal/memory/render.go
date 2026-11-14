package memory

import (
	"strings"

	"github.com/ethosagent/meterwise/internal/contract"
)

// HintPrefix starts every line this package injects into a request.
const HintPrefix = "[proxy] memory: "

// HintMax is the longest hint line, in bytes, prefix included.
const HintMax = 400

// maxHintFiles is how many files a hint names.
const maxHintFiles = 3

// RenderHint is the one-line hint the agent receives: what solved a similar task, the fix and the files. It is rendered
// from the trace each time (the text is never stored with the hit) and cut to HintMax bytes on a character boundary, so
// the session detail shows exactly what the agent got.
func RenderHint(approach, fix string, files []string) string {
	var b strings.Builder
	b.WriteString(HintPrefix)
	b.WriteString("A similar task was solved before: ")
	b.WriteString(oneLine(approach))
	b.WriteString(". Fix: ")
	b.WriteString(oneLine(fix))
	if len(files) > 0 {
		b.WriteString(". Files: ")
		b.WriteString(strings.Join(files[:min(len(files), maxHintFiles)], ", "))
	}
	b.WriteString(".")
	return contract.TruncateUTF8(b.String(), HintMax)
}

// oneLine collapses whitespace so a hint is a single line, and drops a trailing full stop (the template adds its own).
func oneLine(s string) string {
	return strings.TrimRight(strings.Join(strings.Fields(s), " "), ". ")
}
