package trim

import (
	"strings"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Limits of dup_lines: a run needs at least dupMinRuns repeats of a block of up to dupMaxPeriod lines, and the block
// must carry some text (a blank or a rule line is not worth a marker).
const (
	dupMinRuns   = 3
	dupMaxPeriod = 3
	dupMinText   = 8
)

// dupLines collapses consecutive lines (or blocks of two or three lines, such as the frames of a recursive stack
// trace) that are identical once digits are ignored into the first copy and a count: "line (×N)".
type dupLines struct{}

func (dupLines) Name() contract.TrimStage { return contract.StageDupLines }

func (dupLines) Apply(_ Unit, text string) (string, bool) {
	lines := strings.Split(text, "\n")
	if len(lines) < dupMinRuns {
		return "", false
	}
	var b strings.Builder
	b.Grow(len(text))
	changed := false
	for i := 0; i < len(lines); {
		p, runs := dupRunAt(lines, i)
		if runs == 0 {
			b.WriteString(lines[i])
			i++
			if i < len(lines) {
				b.WriteByte('\n')
			}
			continue
		}
		changed = true
		if p == 1 {
			b.WriteString(lines[i])
			b.WriteString(" (×" + itoa(runs) + ")")
		} else {
			for k := 0; k < p; k++ {
				b.WriteString(lines[i+k])
				b.WriteByte('\n')
			}
			b.WriteString("(previous " + itoa(p) + " lines ×" + itoa(runs) + ")")
		}
		i += p * runs
		if i < len(lines) {
			b.WriteByte('\n')
		}
	}
	return b.String(), changed
}

// dupRunAt returns the period and repeat count of the block that starts at lines[i] and repeats at least dupMinRuns
// times; runs is 0 when there is none. Shorter periods win.
func dupRunAt(lines []string, i int) (period, runs int) {
	for p := 1; p <= dupMaxPeriod; p++ {
		if i+p*dupMinRuns > len(lines) || !hasText(lines[i:i+p]) {
			continue
		}
		n := 1
		for i+(n+1)*p <= len(lines) && sameBlock(lines[i:i+p], lines[i+n*p:i+(n+1)*p]) {
			n++
		}
		if n >= dupMinRuns {
			return p, n
		}
	}
	return 0, 0
}

func hasText(block []string) bool {
	n := 0
	for _, l := range block {
		n += substance(l)
	}
	return n >= dupMinText
}

func sameBlock(a, b []string) bool {
	for k := range a {
		if !sameTemplate(a[k], b[k]) {
			return false
		}
	}
	return true
}
