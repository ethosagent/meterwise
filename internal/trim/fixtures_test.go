package trim_test

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The fixtures are the tool outputs the stages are specified against. They are built here, deterministically, and
// the stages' expected output lives in testdata/golden/*.golden (go test ./internal/trim -run Golden -update).

// buildLog is a 300-line make build with three compiler errors and a failing summary.
func buildLog() string {
	var b strings.Builder
	b.WriteString("make -C build all\nmake[1]: Entering directory '/work/build'\n")
	for i := 1; i <= 300; i++ {
		switch i {
		case 87:
			fmt.Fprintf(&b, "src/mod_%d.c:12:5: error: expected ';' before '}' token\n", i)
		case 142:
			fmt.Fprintf(&b, "src/mod_%d.c:40:9: error: 'cfg' undeclared (first use in this function)\n", i)
		case 233:
			fmt.Fprintf(&b, "src/mod_%d.c:7:1: fatal error: missing.h: No such file or directory\n", i)
		default:
			fmt.Fprintf(&b, "[%d/300] CC src/mod_%d.c\n", i, i)
		}
	}
	b.WriteString("make[1]: Leaving directory '/work/build'\nmake: *** [Makefile:12: all] Error 2\n")
	return b.String()
}

// pytestRun is a pytest session: 150 passes, one failure with its report, and the summary.
func pytestRun() string {
	var b strings.Builder
	b.WriteString("============================= test session starts ==============================\nplatform linux -- Python 3.12.3, pytest-8.2.0\ncollected 151 items\n\n")
	for i := 1; i <= 150; i++ {
		fmt.Fprintf(&b, "tests/test_orders.py::test_case_%d PASSED [%3d%%]\n", i, i*100/151)
	}
	b.WriteString("tests/test_orders.py::test_total_with_tax FAILED [100%]\n\n")
	b.WriteString("=================================== FAILURES ===================================\n")
	b.WriteString("______________________________ test_total_with_tax _____________________________\n\n")
	b.WriteString("    def test_total_with_tax():\n>       assert total(100, 0.2) == 120\nE       assert 119.99 == 120\nE        +  where 119.99 = total(100, 0.2)\n\n")
	b.WriteString("tests/test_orders.py:88: AssertionError\n")
	b.WriteString("=========================== short test summary info ============================\n")
	b.WriteString("FAILED tests/test_orders.py::test_total_with_tax - assert 119.99 == 120\n")
	b.WriteString("========================= 1 failed, 150 passed in 3.21s ========================\n")
	return b.String()
}

// gitDiff is `git diff -U12` of two hunks, each with a few changes in a lot of context.
func gitDiff() string {
	var b strings.Builder
	b.WriteString("diff --git a/pkg/engine.go b/pkg/engine.go\nindex 3f2a1b0..9c8d7e6 100644\n--- a/pkg/engine.go\n+++ b/pkg/engine.go\n")
	hunk := func(start int, old, add string) {
		fmt.Fprintf(&b, "@@ -%d,30 +%d,30 @@ func (e *Engine) Run() error {\n", start, start)
		for i := 0; i < 14; i++ {
			fmt.Fprintf(&b, "         step%d := e.steps[%d]\n", start+i, i)
		}
		fmt.Fprintf(&b, "-        %s\n+        %s\n", old, add)
		for i := 14; i < 29; i++ {
			fmt.Fprintf(&b, "         step%d := e.steps[%d]\n", start+i, i)
		}
	}
	hunk(100, "limit := 10", "limit := e.cfg.Limit")
	hunk(220, "return nil", "return e.flush()")
	return b.String()
}

// grepOut is `grep -rn` with 120 matches of one template in a generated file, and two others.
func grepOut() string {
	var b strings.Builder
	b.WriteString("src/main.go:14:func lookup(k string) int {\n")
	for i := 1; i <= 120; i++ {
		fmt.Fprintf(&b, "src/gen/table.go:%d:	%d: lookup(\"key%d\"),\n", 10+i, i, i)
	}
	b.WriteString("src/main.go:90:	return lookup(name)\n")
	return b.String()
}

// stackTrace is a Python RecursionError: 60 identical frame pairs between the first call and the error.
func stackTrace() string {
	var b strings.Builder
	b.WriteString("Traceback (most recent call last):\n  File \"app.py\", line 20, in <module>\n    main()\n")
	for i := 0; i < 60; i++ {
		b.WriteString("  File \"app.py\", line 6, in walk\n    return walk(node.next)\n")
	}
	b.WriteString("RecursionError: maximum recursion depth exceeded\n")
	return b.String()
}

// jsonArray is a list-users response of 12 flat rows with the awkward values a table must keep.
func jsonArray() string {
	rows := make([]map[string]any, 0, 12)
	for i := 1; i <= 12; i++ {
		rows = append(rows, map[string]any{"id": i, "name": fmt.Sprintf("user %d", i), "email": fmt.Sprintf("u%d@example.com", i), "active": i%3 != 0, "score": float64(i) + 0.5, "note": nil})
	}
	rows[3]["name"] = "Doe, Jane"
	rows[4]["name"] = `say "hi"`
	rows[5]["name"] = "007"
	rows[6]["name"] = "true"
	rows[7]["note"] = "line1\nline2"
	b, _ := json.Marshal(rows)
	return string(b)
}

// packageJSON is the file an agent reads twice.
func packageJSON() string {
	return `{"name":"shop","version":"1.4.2","scripts":{"build":"tsc -p .","test":"vitest run","lint":"biome check ."},"dependencies":{"react":"^19.0.0","zod":"^3.23.8"}}` + strings.Repeat(" ", 10)
}
