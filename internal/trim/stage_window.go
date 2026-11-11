package trim

import (
	"strings"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Limits of log_window.
const (
	winMinLines = 40 // shorter output is left alone
	winHead     = 3  // lines kept at the top of a log
	winTail     = 10 // and at the bottom
	winContext  = 2  // lines kept around a line that matters
	winMinFold  = 4  // a run of fewer lines is not worth a marker
)

type outputKind int

const (
	kindNone outputKind = iota
	kindLog
	kindDiff
)

// logWindow keeps what a model acts on in build, test and diff output and folds the rest into "… N lines folded …".
// For a log: the first lines, the last lines, every line that reports an error, a failure or a warning, and two lines
// of context around each. For a diff: headers, hunks and every changed line with two lines of context. Source code and
// anything that does not look like command output is left alone, as is any tool that reads files.
type logWindow struct{}

func (logWindow) Name() contract.TrimStage { return contract.StageLogWindow }

func (logWindow) Apply(u Unit, text string) (string, bool) {
	if readTool(u.Tool) {
		return "", false
	}
	lines := strings.Split(text, "\n")
	if len(lines) < winMinLines {
		return "", false
	}
	kind := classify(lines)
	if kind == kindNone {
		return "", false
	}
	keep, errs := keepSet(lines, kind)
	runs := foldRuns(lines, keep, errs)
	runs = limitFold(lines, runs, len(text), u.MaxFoldPct, errs)
	if len(runs) == 0 {
		return "", false
	}
	var b strings.Builder
	b.Grow(len(text))
	at := 0
	for _, r := range runs {
		for i := at; i < r.a; i++ {
			b.WriteString(lines[i])
			b.WriteByte('\n')
		}
		b.WriteString(foldMarker(r.b-r.a, errs))
		at = r.b
		if at < len(lines) {
			b.WriteByte('\n')
		}
	}
	for i := at; i < len(lines); i++ {
		b.WriteString(lines[i])
		if i < len(lines)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String(), true
}

func foldMarker(n, errs int) string {
	s := "… " + itoa(n) + " lines folded"
	if errs > 0 {
		s += "; " + itoa(errs) + " errors kept"
	}
	return s + " …"
}

// classify decides what kind of output the lines are: a diff (hunks and changed lines), a log (most lines have the
// shape of command output), or neither.
func classify(lines []string) outputKind {
	hunks, changed, shaped := 0, 0, 0
	inherit := false
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "@@"):
			hunks++
		case strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-"):
			changed++
		}
		if l != "" && (l[0] == ' ' || l[0] == '\t') {
			if inherit {
				shaped++
			}
			continue
		}
		inherit = logShaped(l)
		if inherit {
			shaped++
		}
	}
	switch {
	case hunks > 0 && changed > 0:
		return kindDiff
	case shaped*2 >= len(lines):
		return kindLog
	}
	return kindNone
}

// logWords start a line of command output. Digits, '[' and '=' do too (timestamps, "[3/9]", rules).
var logWords = map[string]bool{
	"INFO": true, "WARN": true, "WARNING": true, "ERROR": true, "ERR": true, "DEBUG": true, "TRACE": true, "FATAL": true,
	"PASS": true, "FAIL": true, "FAILED": true, "PASSED": true, "SKIPPED": true, "ok": true, "npm": true, "yarn": true,
	"pnpm": true, "make": true, "cargo": true, "go": true, "Compiling": true, "Building": true, "Downloading": true,
	"Collecting": true, "Installing": true, "Running": true, "Fetching": true, "added": true, "gcc": true, "ld": true,
	"error": true, "warning": true, "note": true, "E": true,
}

func logShaped(l string) bool {
	if l == "" {
		return false
	}
	if c := l[0]; isDigit(c) || c == '[' || c == '=' || c == '>' {
		return true
	}
	end := 0
	for end < len(l) && l[end] != ' ' && l[end] != ':' && l[end] != '=' && l[end] != '\t' {
		end++
	}
	return logWords[l[:end]] || strings.Contains(l, " PASSED") || strings.Contains(l, " FAILED") || strings.Contains(l, "::test")
}

// signal needles, lower case. A strong one counts as an error in the marker.
var needles = [...]struct {
	s      string
	strong bool
}{
	{"error", true}, {"fail", true}, {"fatal", true}, {"panic", true}, {"exception", true}, {"traceback", true},
	{"warn", false}, {"assert", false}, {"denied", false}, {"cannot ", false}, {"undefined", false}, {"segfault", false},
	{"killed", false}, {"timeout", false}, {"timed out", false}, {"not found", false}, {"unable to", false},
}

// needlePair marks the first two bytes (either case) of every needle, so the scan of a line looks at a needle only
// where two bytes in a row can start one.
var needlePair = func() (t [256][256]bool) {
	for _, n := range needles {
		for _, a := range [2]byte{n.s[0], n.s[0] - 'a' + 'A'} {
			if n.s[0] < 'a' || n.s[0] > 'z' {
				a = n.s[0]
			}
			for _, b := range [2]byte{n.s[1], n.s[1] - 'a' + 'A'} {
				if n.s[1] < 'a' || n.s[1] > 'z' {
					b = n.s[1]
				}
				t[a][b] = true
			}
		}
	}
	return t
}()

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

func foldAt(l string, i int, s string) bool {
	if i+len(s) > len(l) {
		return false
	}
	for k := 0; k < len(s); k++ {
		if lower(l[i+k]) != s[k] {
			return false
		}
	}
	return true
}

// signal reports whether a line reports something a model must see, and whether it is an error.
func signal(l string) (found, strong bool) {
	if len(l) > 2 && l[0] == 'E' && l[1] == ' ' && l[2] == ' ' { // pytest's "E   assert ..." lines
		found = true
	}
	for i := 0; i+1 < len(l); i++ {
		if !needlePair[l[i]][l[i+1]] {
			continue
		}
		for _, n := range needles {
			if foldAt(l, i, n.s) {
				found = true
				if n.strong {
					return true, true
				}
			}
		}
	}
	return found, false
}

// diffKept are the prefixes of diff lines that always stay.
var diffKept = [...]string{"diff ", "index ", "--- ", "+++ ", "@@", "\\", "new file", "deleted file", "similarity", "rename ", "Binary files"}

// keepSet marks the lines to keep and counts the error lines among them.
func keepSet(lines []string, kind outputKind) (keep []bool, errs int) {
	keep = make([]bool, len(lines))
	mark := func(i int) {
		for j := max(0, i-winContext); j <= min(len(lines)-1, i+winContext); j++ {
			keep[j] = true
		}
	}
	if kind == kindLog {
		for i := 0; i < min(winHead, len(lines)); i++ {
			keep[i] = true
		}
		for i := max(0, len(lines)-winTail); i < len(lines); i++ {
			keep[i] = true
		}
	}
	for i, l := range lines {
		if kind == kindDiff {
			switch {
			case isDiffChange(l):
				mark(i)
			case isDiffHeader(l):
				keep[i] = true
			}
			continue
		}
		if found, strong := signal(l); found {
			mark(i)
			if strong {
				errs++
			}
		}
	}
	return keep, errs
}

// isDiffChange reports an added or removed line (not a file header).
func isDiffChange(l string) bool {
	return strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++ ") || strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "--- ")
}

// isDiffHeader reports a line of the diff's own structure, which always stays.
func isDiffHeader(l string) bool {
	for _, p := range diffKept {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

// run is the half-open range of lines [a, b) to fold.
type run struct{ a, b int }

// foldRuns lists the runs of unkept lines worth a marker: at least winMinFold lines, and the marker must be shorter.
func foldRuns(lines []string, keep []bool, errs int) []run {
	var out []run
	for i := 0; i < len(lines); {
		if keep[i] {
			i++
			continue
		}
		j := i
		for j < len(lines) && !keep[j] {
			j++
		}
		if j-i >= winMinFold && runBytes(lines, run{i, j}) > len(foldMarker(j-i, errs))+1 {
			out = append(out, run{i, j})
		}
		i = j
	}
	return out
}

func runBytes(lines []string, r run) int {
	n := 0
	for i := r.a; i < r.b; i++ {
		n += len(lines[i]) + 1
	}
	return n
}

// markerLen is len(foldMarker(n, errs)) without building it.
func markerLen(n, errs int) int {
	l := len("… ") + digits(n) + len(" lines folded") + len(" …")
	if errs > 0 {
		l += len("; ") + digits(errs) + len(" errors kept")
	}
	return l
}

func digits(n int) int {
	d := 1
	for ; n >= 10; n /= 10 {
		d++
	}
	return d
}

// limitFold gives lines back so the folded part is at most maxPct percent of the text: it restores the leading
// lines of the runs, in order, until the limit holds, and drops a run left with fewer than winMinFold lines. The
// folded part is the bytes the runs hold less their markers.
func limitFold(lines []string, runs []run, total, maxPct int, errs int) []run {
	allowed := total * maxPct / 100
	net := func(r run, bytes int) int { return bytes - markerLen(r.b-r.a, errs) - 1 }
	bytes := make([]int, len(runs))
	folded := 0
	for i, r := range runs {
		bytes[i] = runBytes(lines, r)
		folded += net(r, bytes[i])
	}
	for i := 0; i < len(runs) && folded > allowed; {
		folded -= net(runs[i], bytes[i])
		bytes[i] -= len(lines[runs[i].a]) + 1
		runs[i].a++
		if runs[i].b-runs[i].a < winMinFold {
			runs, bytes = append(runs[:i], runs[i+1:]...), append(bytes[:i], bytes[i+1:]...)
			continue
		}
		folded += net(runs[i], bytes[i])
	}
	return runs
}
