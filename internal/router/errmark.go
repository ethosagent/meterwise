package router

import (
	"strings"

	"github.com/ethosagent/tollgate"
)

// errWindow is how many bytes of a tool result are read from each end: failures sit at the top (a command's first
// line) or the bottom (a test summary), and a result can be megabytes.
const errWindow = 4096

// lastToolFailed reports whether any tool result after the newest assistant turn looks like a failure. It is a
// heuristic over the text only, so it is deliberately narrow: a source file that merely contains the word "error"
// must not hold every read-heavy session, so a result counts only when a line starts like a failure report
// (Traceback, "error:", "panic:", "FAIL", "npm ERR!", a non-zero exit status) or a language's "SomethingError:".
func lastToolFailed(msgs []tollgate.Message) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		switch msgs[i].Role {
		case tollgate.RoleAssistant:
			return false
		case tollgate.RoleTool:
			if failureText(msgs[i].Text) {
				return true
			}
		}
	}
	return false
}

// failureText applies the line rules to the first and last errWindow bytes of s.
func failureText(s string) bool {
	if len(s) <= 2*errWindow {
		return anyFailureLine(s)
	}
	return anyFailureLine(s[:errWindow]) || anyFailureLine(s[len(s)-errWindow:])
}

func anyFailureLine(s string) bool {
	for len(s) > 0 {
		line, rest, _ := strings.Cut(s, "\n")
		if failureLine(strings.TrimSpace(line)) {
			return true
		}
		s = rest
	}
	return false
}

// failurePrefixes are lowercase line starts that report a failure.
var failurePrefixes = []string{
	"traceback (most recent call last)", "error:", "error[", "fatal:", "fatal error", "panic:", "--- fail", "fail\t", "fail ",
	"npm err!", "command failed", "command not found", "exception in thread", "unhandled exception", "segmentation fault",
	"build failed", "compilation failed", "make: ***", "permission denied", "no such file or directory",
}

// shellPrefixes start a shell's or a package manager's own line, which is a failure only when it says so.
var shellPrefixes = []string{"e: ", "bash: ", "zsh: "}

func failureLine(line string) bool {
	if line == "" {
		return false
	}
	if len(line) > 160 {
		line = line[:160]
	}
	low := strings.ToLower(line)
	for _, p := range failurePrefixes {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	for _, p := range shellPrefixes {
		if strings.HasPrefix(low, p) {
			return strings.Contains(low, "error") || strings.Contains(low, "not found") || strings.Contains(low, "failed")
		}
	}
	for _, p := range []string{"exit status ", "exit code ", "exit code: ", "exited with code ", "exited with status "} {
		if rest, ok := strings.CutPrefix(low, p); ok {
			return rest != "" && rest[0] >= '1' && rest[0] <= '9'
		}
	}
	return namedError(line)
}

// namedError matches "TypeError: ...", "ModuleNotFoundError: ...", "java.lang.NullPointerException: ...": one
// identifier ending in Error or Exception, then a colon, at the start of a line.
func namedError(line string) bool {
	head, _, ok := strings.Cut(line, ":")
	if !ok || head == "" || strings.ContainsAny(head, " \t") {
		return false
	}
	return strings.HasSuffix(head, "Error") || strings.HasSuffix(head, "Exception")
}
