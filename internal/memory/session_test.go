package memory

import (
	"fmt"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
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

func gateCall(attrs string, meta map[string]any) *tollgate.Call {
	return &tollgate.Call{
		Tenant:   tollgate.Tenant{ID: "w", Attrs: map[string]string{AttrMemory: attrs}, Policy: &tollgate.Policy{Capture: true}},
		Session:  tollgate.Session{ID: "s"},
		Provider: tollgate.Provider{Wire: tollgate.WireOpenAIChat}, Meta: meta,
	}
}

func TestAfterReusesTheConfigBeforeResolved(t *testing.T) {
	on := false
	e := &Engine{opt: Options{PrivacyMode: &on}}
	hint := contract.DefaultMemoryConfig()
	hint.Mode = contract.MemoryModeHint
	off := contract.DefaultMemoryConfig()
	off.Mode = contract.MemoryModeOff

	c := gateCall(off.Attr(), map[string]any{metaConfig: hint})
	if cfg, ok := e.gate(c, false); !ok || cfg.Mode != contract.MemoryModeHint {
		t.Errorf("After gate = %v, %v: want the stashed hint config", cfg.Mode, ok)
	}
	if _, ok := e.gate(c, true); ok {
		t.Error("Before must resolve the settings itself, not trust a stash")
	}
	if cfg, ok := e.gate(gateCall(hint.Attr(), nil), false); !ok || cfg.Mode != contract.MemoryModeHint {
		t.Errorf("After without a stash must resolve the settings: %v, %v", cfg.Mode, ok)
	}
}

func TestConfigMemoIsBounded(t *testing.T) {
	e := &Engine{}
	for i := range 3 * maxCfgs {
		c := contract.DefaultMemoryConfig()
		c.MaxHintsPerSession = i%5 + 1
		c.Model = fmt.Sprintf("model-%d", i)
		if got := e.config(gateCall(c.Attr(), nil)); got.Model != c.Model {
			t.Fatalf("config %d = %+v", i, got)
		}
	}
	n := 0
	e.cfgs.Range(func(_, _ any) bool { n++; return true })
	if n > maxCfgs {
		t.Errorf("memo holds %d entries, want at most %d", n, maxCfgs)
	}
}
