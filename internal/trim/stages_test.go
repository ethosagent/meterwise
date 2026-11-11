package trim_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/trim"
)

var update = flag.Bool("update", false, "rewrite testdata/golden")

func settings() trim.Settings {
	en := map[contract.TrimStage]bool{}
	for _, s := range contract.TrimStages() {
		en[s] = s != contract.StageSchemaTrim
	}
	return trim.Settings{MinGainPct: 2, MaxFoldPct: 90, Enabled: en}
}

func only(s contract.TrimStage) trim.Settings {
	st := settings()
	for k := range st.Enabled {
		st.Enabled[k] = k == s
	}
	return st
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from its golden file\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// T1: each stage has a golden fixture; what it keeps is asserted explicitly as well, so a wrong golden cannot hide a bug.
func TestGoldenStages(t *testing.T) {
	p := trim.NewPipeline()
	cases := []struct {
		name   string
		in     string
		unit   trim.Unit
		stage  contract.TrimStage
		keeps  []string
		drops  []string
		fewer  int // the output has at most this many lines
		bytesX int // and is at most 1/bytesX of the input
	}{
		{"build_log", buildLog(), trim.Unit{Tool: "bash"}, contract.StageLogWindow,
			[]string{"src/mod_87.c:12:5: error", "src/mod_142.c:40:9: error", "src/mod_233.c:7:1: fatal error", "make: *** [Makefile:12: all] Error 2", "4 errors kept"},
			[]string{"[50/300] CC src/mod_50.c", "[200/300] CC src/mod_200.c"}, 80, 4},
		{"pytest", pytestRun(), trim.Unit{Tool: "bash"}, contract.StageLogWindow,
			[]string{"test_total_with_tax FAILED", "E       assert 119.99 == 120", "1 failed, 150 passed", "tests/test_orders.py:88: AssertionError"},
			[]string{"test_case_70 PASSED"}, 60, 3},
		{"git_diff", gitDiff(), trim.Unit{Tool: "bash"}, contract.StageLogWindow,
			[]string{"diff --git a/pkg/engine.go", "-        limit := 10", "+        limit := e.cfg.Limit", "+        return e.flush()", "@@ -220,30 +220,30 @@"},
			[]string{"step105 := e.steps[5]"}, 60, 2},
		{"grep", grepOut(), trim.Unit{Tool: "bash"}, contract.StageDupLines,
			[]string{"src/main.go:14:func lookup", "src/main.go:90:", "(×120)"}, []string{"key77"}, 5, 10},
		{"stack_trace", stackTrace(), trim.Unit{Tool: "bash"}, contract.StageDupLines,
			[]string{"Traceback (most recent call last):", "RecursionError: maximum recursion depth exceeded", "(previous 2 lines ×60)", `File "app.py", line 6, in walk`}, nil, 10, 10},
		{"json_array", jsonArray(), trim.Unit{Tool: "api"}, contract.StageJSONTable,
			[]string{"[12]{active,email,id,name,note,score}:", `"Doe, Jane"`, `"007"`, `"true"`, `"line1\nline2"`, "u12@example.com"}, nil, 14, 2},
		{"repeated_read", packageJSON(), trim.Unit{Tool: "bash", Seen: 3}, contract.StageRepeatCollapse,
			[]string{"[unchanged since step 3 — see above]"}, nil, 1, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := p.Run(tc.unit, tc.in, withFold(only(tc.stage)))
			if len(out.Stages) != 1 || out.Stages[0].Stage != tc.stage || out.Stages[0].Reverted {
				t.Fatalf("stages = %+v, want only %s", out.Stages, tc.stage)
			}
			for _, k := range tc.keeps {
				if !strings.Contains(out.Text, k) {
					t.Errorf("output lost %q", k)
				}
			}
			for _, d := range tc.drops {
				if strings.Contains(out.Text, d) {
					t.Errorf("output still has %q", d)
				}
			}
			if n := strings.Count(out.Text, "\n") + 1; n > tc.fewer {
				t.Errorf("output has %d lines, want at most %d", n, tc.fewer)
			}
			if len(out.Text)*tc.bytesX > len(tc.in) && tc.name != "repeated_read" {
				t.Errorf("output is %d of %d bytes, want at most 1/%d", len(out.Text), len(tc.in), tc.bytesX)
			}
			golden(t, tc.name, out.Text)
			again := p.Run(tc.unit, tc.in, withFold(only(tc.stage)))
			if again.Text != out.Text {
				t.Error("the stage is not deterministic")
			}
		})
	}
}

func withFold(s trim.Settings) trim.Settings { s.MaxFoldPct = 100; return s }

// A table re-parses to the rows it was made from, quoting included.
func TestJSONTableReparsesToTheOriginalRows(t *testing.T) {
	in := jsonArray()
	out := trim.NewPipeline().Run(trim.Unit{}, in, only(contract.StageJSONTable))
	tab, err := trim.DecodeTable(out.Text)
	if err != nil {
		t.Fatal(err)
	}
	var want, got []map[string]any
	if err := json.Unmarshal([]byte(in), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(tab.JSON()), &got); err != nil {
		t.Fatalf("%v: %s", err, tab.JSON())
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("rows differ:\n got %v\nwant %v", got, want)
	}
}

func TestJSONTableLeavesWhatItCannotRepresent(t *testing.T) {
	rows := func(n int, row string) string { return "[" + strings.TrimSuffix(strings.Repeat(row+",", n), ",") + "]" }
	for name, in := range map[string]string{
		"nested value":   rows(8, `{"a":1,"b":{"c":2}}`),
		"array value":    rows(8, `{"a":1,"b":[1]}`),
		"too few rows":   rows(3, `{"a":1,"b":"x"}`),
		"scalars":        rows(8, `1`),
		"different keys": `[{"a":1},{"a":1},{"b":1},{"a":1},{"a":1},{"a":1}]`,
		"key order":      `[{"a":1,"b":2},{"a":1,"b":2},{"b":2,"a":1},{"a":1,"b":2},{"a":1,"b":2},{"a":1,"b":2}]`,
		"empty objects":  rows(8, `{}`),
		"not json":       "[{a:1}, {a:2}, {a:3}, {a:4}, {a:5}, {a:6}]",
		"trailing text":  rows(8, `{"a":1,"b":"x"}`) + " and more",
		"out of range":   rows(8, `{"a":1e999,"b":"x"}`),
	} {
		t.Run(name, func(t *testing.T) {
			if out := trim.NewPipeline().Run(trim.Unit{}, in, only(contract.StageJSONTable)); out.Text != in || len(out.Stages) != 0 {
				t.Errorf("rewrote %q to %q (%+v)", in, out.Text, out.Stages)
			}
		})
	}
}

// Source code is not command output: log_window leaves it, and anything a file-reading tool returns.
func TestLogWindowLeavesSourceCodeAndFileReads(t *testing.T) {
	var b strings.Builder
	b.WriteString("package main\n\nimport \"fmt\"\n\n")
	for i := 0; i < 80; i++ {
		b.WriteString("func f" + strings.Repeat("x", i%5) + "() error {\n\tif err != nil {\n\t\treturn fmt.Errorf(\"failed to read: %w\", err)\n\t}\n\treturn nil\n}\n")
	}
	src := b.String()
	p := trim.NewPipeline()
	if out := p.Run(trim.Unit{Tool: "bash"}, src, only(contract.StageLogWindow)); out.Text != src {
		t.Error("source code was windowed")
	}
	if out := p.Run(trim.Unit{Tool: "read_file"}, buildLog(), only(contract.StageLogWindow)); out.Text != buildLog() {
		t.Error("a file read was windowed")
	}
	if out := p.Run(trim.Unit{Tool: "bash"}, "short output\nerror: x\n", only(contract.StageLogWindow)); len(out.Stages) != 0 {
		t.Error("short output was windowed")
	}
}

// MaxFoldPct is a ceiling: lowering it gives lines back, and the output never folds more than it allows.
func TestLogWindowHonoursMaxFoldPct(t *testing.T) {
	in := buildLog()
	p := trim.NewPipeline()
	prev := 0
	for _, pct := range []int{100, 90, 70, 40} {
		st := only(contract.StageLogWindow)
		st.MaxFoldPct, st.MinGainPct = pct, 0
		out := p.Run(trim.Unit{Tool: "bash"}, in, st)
		folded := len(in) - len(out.Text)
		if folded > len(in)*pct/100+64 { // the markers add a few bytes back
			t.Errorf("max %d%%: folded %d of %d bytes", pct, folded, len(in))
		}
		if pct <= 70 && len(out.Text) <= prev {
			t.Errorf("max %d%%: output did not grow (%d <= %d)", pct, len(out.Text), prev)
		}
		for _, k := range []string{"src/mod_87.c:12:5: error", "src/mod_142.c:40:9: error", "src/mod_233.c:7:1: fatal error"} {
			if !strings.Contains(out.Text, k) {
				t.Errorf("max %d%%: lost %q", pct, k)
			}
		}
		prev = len(out.Text)
	}
}

// The per-region quality gate: a stage that saves less than MinGainPct of the region is reverted, and its output unused.
func TestMinGainRevertsAWeakStage(t *testing.T) {
	in := strings.Repeat("a line that stays the same size\n", 40) + "x 1\nx 2\nx 3\n"
	st := only(contract.StageDupLines)
	st.MinGainPct = 2
	out := trim.NewPipeline().Run(trim.Unit{}, in, st)
	if out.Text == in || len(out.Stages) != 1 || out.Stages[0].Reverted {
		t.Fatalf("the 40-line run saves more than 2%%: %+v", out.Stages)
	}
	small := strings.Repeat("k ", 2000) + "\nline 1 of text\nline 2 of text\nline 3 of text\n"
	out = trim.NewPipeline().Run(trim.Unit{}, small, st)
	if out.Text != small || len(out.Stages) != 1 || !out.Stages[0].Reverted || out.Stages[0].After != out.Stages[0].Before {
		t.Fatalf("a stage under 2%% must be reverted, got %+v", out.Stages)
	}
	st.MinGainPct = 0
	if out = trim.NewPipeline().Run(trim.Unit{}, small, st); out.Text == small {
		t.Error("with MinGainPct 0 any gain counts")
	}
}

// All stages together, in order: a repeat is only a marker, a log is windowed and then deduplicated.
func TestStagesRunInOrder(t *testing.T) {
	p := trim.NewPipeline()
	out := p.Run(trim.Unit{Tool: "bash", Seen: 2}, buildLog(), settings())
	if len(out.Stages) != 1 || out.Stages[0].Stage != contract.StageRepeatCollapse {
		t.Errorf("a repeat ends at repeat_collapse: %+v", out.Stages)
	}
	out = p.Run(trim.Unit{Tool: "bash"}, buildLog(), settings())
	if len(out.Stages) == 0 || out.Stages[0].Stage != contract.StageLogWindow {
		t.Errorf("a log starts with log_window: %+v", out.Stages)
	}
	for i := 1; i < len(out.Stages); i++ {
		if out.Stages[i].Before != out.Stages[i-1].After {
			t.Errorf("stage %d starts at %d, the one before ended at %d", i, out.Stages[i].Before, out.Stages[i-1].After)
		}
	}
}

func TestSchemaTrimShortensOnlyOverlongDescriptions(t *testing.T) {
	st := settings()
	st.Enabled[contract.StageSchemaTrim] = true
	long := strings.Repeat("Reads a file from the workspace and returns its content. ", 12) + "Éé"
	out, ok := trim.NewPipeline().Description("read", long, st)
	if !ok || len(out.Text) >= 260 || !strings.HasSuffix(out.Text, " …") || !strings.HasPrefix(long, strings.TrimSuffix(out.Text, " …")) {
		t.Fatalf("got %q (%v)", out.Text, ok)
	}
	if _, ok := trim.NewPipeline().Description("read", "short description", st); ok {
		t.Error("a short description was cut")
	}
	st.Enabled[contract.StageSchemaTrim] = false
	if _, ok := trim.NewPipeline().Description("read", long, st); ok {
		t.Error("schema_trim ran while disabled")
	}
}
