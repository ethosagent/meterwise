package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// memoryRun plays the demo once with the given -memory mode and returns the database handle and workspace.
func memoryRun(t *testing.T, mode string) (result, string, func(q string, args ...any) int) {
	t.Helper()
	dsn, st, ws := demoDB(t)
	var out bytes.Buffer
	res, err := run(context.Background(), config{dsn: dsn, rounds: 1, memory: mode}, &out)
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	return res, ws, func(q string, args ...any) int { return scalar(t, st, q, append([]any{ws}, args...)...) }
}

func TestHintDemoStoresATraceFromSessionAAndInjectsItIntoSessionB(t *testing.T) {
	res, _, n := memoryRun(t, "hint")
	if got := n(`SELECT count(*) FROM memory_traces WHERE workspace_id = $1 AND session_id LIKE '%-memory-a'`); got != 1 {
		t.Fatalf("%d traces stored for session A, want 1", got)
	}
	if got := n(`SELECT count(*) FROM memory_hits WHERE workspace_id = $1 AND injected AND mode = 'hint' AND session_id LIKE '%-memory-b'`); got != 1 {
		t.Fatalf("%d injected hits in session B, want 1", got)
	}
	if got := n(`SELECT count(*) FROM memory_hits WHERE workspace_id = $1 AND session_id LIKE '%-memory-a'`); got != 0 {
		t.Errorf("session A had %d hits; there was nothing to recall yet", got)
	}
	if got := n(`SELECT count(*) FROM memory_self_usage WHERE workspace_id = $1 AND kind = 'distil'`); got < 1 {
		t.Errorf("the distil call was not metered (%d rows)", got)
	}
	if got := n(`SELECT count(*) FROM calls WHERE workspace_id = $1 AND session_id LIKE '%-memory-%'`); got != 10 {
		t.Errorf("%d memory-session calls (a: %d, b: %d), want 5 per session", got, n(`SELECT count(*) FROM calls WHERE workspace_id = $1 AND session_id LIKE '%-memory-a'`), n(`SELECT count(*) FROM calls WHERE workspace_id = $1 AND session_id LIKE '%-memory-b'`))
	}
	if res.clientCalls == 0 {
		t.Error("nothing was sent")
	}
}

func TestObserveDemoRecordsTheHitButEditsNothing(t *testing.T) {
	_, _, n := memoryRun(t, "observe")
	if got := n(`SELECT count(*) FROM memory_hits WHERE workspace_id = $1 AND NOT injected AND mode = 'observe'`); got != 1 {
		t.Fatalf("%d observed hits, want 1", got)
	}
	if got := n(`SELECT count(*) FROM memory_hits WHERE workspace_id = $1 AND injected`); got != 0 {
		t.Errorf("observe mode injected %d hints", got)
	}
}

func TestTheDefaultFlagsPlayNoMemoryScenario(t *testing.T) {
	_, _, n := memoryRun(t, "")
	if got := n(`SELECT count(*) FROM memory_traces WHERE workspace_id = $1`) + n(`SELECT count(*) FROM memory_hits WHERE workspace_id = $1`); got != 0 {
		t.Fatalf("memory off yet %d trace and hit rows", got)
	}
	if got := n(`SELECT count(*) FROM calls WHERE workspace_id = $1 AND session_id LIKE '%-memory-%'`); got != 0 {
		t.Errorf("memory off yet %d memory-session calls", got)
	}
}

func TestTheModelTwinChecksItsKeyAndNeverSeesTheCallersKey(t *testing.T) {
	tw := newModelTwin("k")
	defer tw.Close()
	for _, p := range []string{"/embeddings", "/chat/completions"} {
		resp, err := tw.Client().Post(tw.URL+p, "application/json", strings.NewReader(`{"input":"x"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("%s without the key: %d, want 401", p, resp.StatusCode)
		}
	}
	if tw.rejected.Load() != 2 {
		t.Errorf("rejected %d, want 2", tw.rejected.Load())
	}
}
