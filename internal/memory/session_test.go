package memory

import (
	"testing"

	"github.com/ethosagent/tollgate"
)

func TestIsWriteToolUsesTheSharedClassifier(t *testing.T) {
	for name, want := range map[string]bool{
		"edit_file": true, "Write": true, "str_replace_editor": true, "apply_patch": true,
		"Bash": true, "shell": true, "exec_command": true, "git_commit": true, // the old substring list missed these
		"replacement_info": false, "recreation_log": false, // and false-positived on these
		"read_file": false, "grep": false, "": false,
	} {
		if got := isWriteTool(name); got != want {
			t.Errorf("isWriteTool(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestSessionWroteFollowsToolKind(t *testing.T) {
	for name, want := range map[string]bool{"Bash": true, "edit_file": true, "read_file": false, "replacement_info": false} {
		st := &sess{}
		st.tool(tollgate.ToolCall{ID: "1", Name: name})
		if st.wrote != want {
			t.Errorf("a %q call: wrote = %v, want %v", name, st.wrote, want)
		}
	}
}

func TestErrorLinesFollowTheSharedNeedles(t *testing.T) {
	for line, want := range map[string]bool{
		"build failed: exit 2": true, "FATAL: out of memory": true, "panic: nil dereference": true, "permission denied": true,
		"request timed out": true, "unable to open the file": true, "undefined: foo": true, "file not found": true,
		"warning: unused variable": false, "assertion ok": false, "all tests passed": false,
	} {
		st := &sess{}
		st.errorOf(line)
		if got := len(st.sum.Errors) == 1; got != want {
			t.Errorf("errorOf(%q) recorded = %v, want %v", line, got, want)
		}
	}
}
