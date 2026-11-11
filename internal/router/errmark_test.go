package router_test

import (
	"strings"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/router"
)

// What counts as a failing tool result is read through the hook: a failure holds the call (error_retry), anything
// else leaves it routable.
func TestFailureMarkers(t *testing.T) {
	big := strings.Repeat("ok line\n", 3000)
	cases := []struct {
		name, text string
		fail       bool
	}{
		{"python traceback", "Traceback (most recent call last):\n  File \"a.py\", line 1", true},
		{"named error", "ModuleNotFoundError: No module named 'x'", true},
		{"go test failure", "--- FAIL: TestX (0.00s)\nFAIL\tgithub.com/x/y\t0.1s", true},
		{"panic", "panic: runtime error: index out of range", true},
		{"compiler error", "main.go:3:1: error: expected ';'\nerror: aborting", true},
		{"npm", "npm ERR! code ELIFECYCLE", true},
		{"nonzero exit", "output...\nexit status 2", true},
		{"exit code colon", "exit code: 127", true},
		{"failure at the tail of a long result", big + "FAIL\tpkg\t1s", true},
		{"failure at the head of a long result", "error: boom\n" + big, true},
		{"zero exit", "exit status 0", false},
		{"source code that mentions error", "func f() error {\n\treturn fmt.Errorf(\"error: %w\", err)\n}", false},
		{"a doc about errors", "Handling errors\nerror handling is described below", false},
		{"a failure buried mid-way of a long result is not read", big[:len(big)/2] + "error: hidden\n" + big, false},
		{"a file listing", "errors.go\nerror_test.go\nmain.go", false},
		{"empty", "", false},
		{"shell prefix without a failure word", "bash: line 1 of the script", false},
		{"shell not found", "bash: foo: command not found", true},
	}
	h := router.New(router.Options{Tables: tables()})
	cons := attr(contract.RouterModeConservative)
	for _, tc := range cases {
		c := callAt(5, "gpt-4.1", openaiBody, cons)
		c.Messages = []tollgate.Message{{Role: tollgate.RoleUser, Text: "go"}, {Role: tollgate.RoleAssistant}, {Role: tollgate.RoleTool, Text: tc.text}}
		d := before(t, h, c)
		if got := d.Reason == contract.HeldErrorRetry; got != tc.fail {
			t.Errorf("%s: held for error = %v, want %v (%+v)", tc.name, got, tc.fail, d)
		}
	}
	// only the newest turn's results count: a failure before the last assistant turn is history
	c := callAt(5, "gpt-4.1", openaiBody, cons)
	c.Messages = []tollgate.Message{{Role: tollgate.RoleTool, Text: "error: old"}, {Role: tollgate.RoleAssistant}, {Role: tollgate.RoleTool, Text: "fine"}}
	if d := before(t, h, c); d.Reason == contract.HeldErrorRetry {
		t.Errorf("an old failure held the call: %+v", d)
	}
}
