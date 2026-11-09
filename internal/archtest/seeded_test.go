package archtest

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

const (
	cn = module + "/internal/contract"
	st = module + "/internal/store"
	ap = module + "/internal/api"
)

func rules(vs []Violation) []string {
	seen := map[string]bool{}
	for _, v := range vs {
		seen[v.Rule] = true
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// seeds: one deliberately bad input per rule; analyze must report that rule.
// "green" rows are the legal neighbours of the same rule: they must stay clean.
func TestSeededViolationsGoRed(t *testing.T) {
	tests := []struct {
		name string
		pkg  pkg
		want string // rule that must fire; "" = must be clean
	}{
		{"D1 package outside every layer", pkg{Rel: "internal/misc", Doc: "x"}, "D1"},
		{"D1 green: known layer", pkg{Rel: "internal/tenancy", Doc: "x"}, ""},
		{"P1 missing doc comment", pkg{Rel: "internal/tenancy"}, "P1"},
		{"H0 green: contract may use any stdlib package", pkg{Rel: "internal/contract", Doc: "x", Imports: []string{"net/http"}}, ""},
		{"H0 contract imports a feature package", pkg{Rel: "internal/contract", Doc: "x", Imports: []string{module + "/internal/audit"}}, "H0"},
		{"H0 contract imports a third party", pkg{Rel: "internal/contract", Doc: "x", Imports: []string{"github.com/jackc/pgx/v5"}}, "H0"},
		{"H0 green: contract imports tollgate root and stdlib", pkg{Rel: "internal/contract", Doc: "x", Imports: []string{"fmt", tollgate}}, ""},
		{"H1 imports tollgate internal", pkg{Rel: "internal/dataplane", Doc: "x", Imports: []string{tollgate + "/internal/forward"}}, "H1"},
		{"H1 test imports tollgate internal", pkg{Rel: "internal/dataplane", Doc: "x", TestImports: []string{tollgate + "/internal/model"}}, "H1"},
		{"H1 production imports testkit", pkg{Rel: "internal/dataplane", Doc: "x", Imports: []string{tollgate + "/testkit"}}, "H1"},
		{"H1 green: test imports testkit", pkg{Rel: "internal/dataplane", Doc: "x", TestImports: []string{tollgate + "/testkit"}}, ""},
		{"H1 tollgate cmd is not public", pkg{Rel: "internal/dataplane", Doc: "x", Imports: []string{tollgate + "/cmd/tollgate-bench"}}, "H1"},
		{"H1 tollgate unknown subpackage is not public", pkg{Rel: "internal/dataplane", Doc: "x", Imports: []string{tollgate + "/fixtures"}}, "H1"},
		{"H1 green: root and ext", pkg{Rel: "internal/dataplane", Doc: "x", Imports: []string{tollgate, tollgate + "/ext/auth"}}, ""},
		{"H2 store imports api", pkg{Rel: "internal/store", Doc: "x", Imports: []string{ap}}, "H2"},
		{"H2 tenancy imports ledger", pkg{Rel: "internal/tenancy", Doc: "x", Imports: []string{module + "/internal/ledger"}}, "H2"},
		{"H2 anything imports cmd", pkg{Rel: "internal/api", Doc: "x", Imports: []string{module + "/cmd/meterwised"}}, "H2"},
		{"H2 anything imports archtest", pkg{Rel: "internal/api", Doc: "x", Imports: []string{module + "/internal/archtest"}}, "H2"},
		{"H2 green: cmd imports api, api imports store", pkg{Rel: "cmd/meterwised", Doc: "x", Imports: []string{ap, st, cn}}, ""},
		{"H3 history imports store", pkg{Rel: "internal/history", Doc: "x", Imports: []string{st}}, "H3"},
		{"H3 monitors imports net/http", pkg{Rel: "internal/monitors", Doc: "x", Imports: []string{"net/http"}}, "H3"},
		{"H3 savings imports database/sql", pkg{Rel: "internal/savings", Doc: "x", Imports: []string{"database/sql"}}, "H3"},
		{"H3 savings imports another feature", pkg{Rel: "internal/savings", Doc: "x", Imports: []string{module + "/internal/audit"}}, "H3"},
		{"H3 green: stdlib, contract, tollgate root", pkg{Rel: "internal/monitors", Doc: "x", Imports: []string{"sync", "strings", cn, tollgate}}, ""},
		{"H6 unlisted third party", pkg{Rel: "internal/tenancy", Doc: "x", Imports: []string{"github.com/gin-gonic/gin"}}, "H6"},
		{"H6 listed third party in the wrong layer", pkg{Rel: "internal/ledger", Doc: "x", Imports: []string{"github.com/jackc/pgx/v5"}}, "H6"},
		{"H6 green: pgx in store", pkg{Rel: "internal/store", Doc: "x", Imports: []string{"github.com/jackc/pgx/v5/pgxpool"}}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rules(analyze([]pkg{tc.pkg}))
			switch {
			case tc.want == "" && len(got) != 0:
				t.Fatalf("expected clean, got %v", got)
			case tc.want != "" && !slices.Contains(got, tc.want):
				t.Fatalf("expected %s to fire, got %v", tc.want, got)
			}
		})
	}
}

func TestSeededWriterViolationGoesRed(t *testing.T) {
	bad := map[string]string{"internal/tenancy/q.go": "const q = `INSERT INTO calls (id) VALUES ($1)`"}
	if got := rules(scanWriters(bad)); !slices.Equal(got, []string{"H4"}) {
		t.Fatalf("bad input: %v", got)
	}
	for name, body := range map[string]string{
		"internal/store/migrations/0001_x.sql":  "UPDATE sessions SET x = 1",
		"internal/store/queries/workspaces.sql": "DELETE FROM  public.monitor_events",
		"internal/access/a.go":                  "insert into audit_log values (1)",
	} {
		if got := scanWriters(map[string]string{name: body}); len(got) != 1 {
			t.Errorf("%s: expected one H4, got %v", name, got)
		}
	}
	for name, body := range map[string]string{
		"internal/tenancy/a.go":  "const q = `INSERT INTO \"calls\" (id) VALUES (1)`",
		"internal/tenancy/b.sql": "UPDATE ONLY sessions SET n = 1",
		"internal/tenancy/c.sql": "DELETE FROM ONLY calls",
		"internal/tenancy/d.sql": "MERGE INTO monitor_events USING x ON true",
		"internal/tenancy/e.sql": "TRUNCATE TABLE audit_log",
		"internal/tenancy/f.sql": "COPY public.\"calls\" FROM STDIN",
	} {
		if got := scanWriters(map[string]string{name: body}); len(got) != 1 {
			t.Errorf("%s (%s): expected one H4, got %v", name, body, got)
		}
	}
	good := map[string]string{
		"internal/config/prose.go":            "// we update sessions elsewhere; delete from calls is the ledger's job\nvar x = 1",
		"internal/config/prose.sql":           "-- INSERT INTO calls is done by ledger\nSELECT 1;",
		"internal/store/queries/ledger.sql":   "INSERT INTO calls (id) VALUES ($1)",
		"internal/store/sqlc/audit.sql.go":    "INSERT INTO audit_log",
		"internal/ledger/write.go":            "UPDATE sessions SET n = n + 1",
		"internal/ledger/write_test.go":       "INSERT INTO calls",
		"internal/tenancy/reads.sql":          "SELECT * FROM calls",
		"internal/tenancy/q_test.go":          "INSERT INTO sessions",
		"internal/store/queries/sessions.sql": "-- reads only: SELECT id FROM sessions",
	}
	if got := scanWriters(good); len(got) != 0 {
		t.Fatalf("legal writers flagged: %v", got)
	}
}

func TestSeededWebViolationsGoRed(t *testing.T) {
	bad := map[string]string{
		"web/src/a.ts":            "import _ from 'lodash'",
		"web/src/b.tsx":           "import { x } from '../../server/secret'",
		"web/src/c.ts":            "const m = await import(\"left-pad\")",
		"web/src/d.ts":            "export * from '/abs/path'",
		"web/src/features/e/e.ts": "import q from '@scope/pkg/deep'",
		"web/src/zz.js":           "import x from 'left-pad-js'",
		"web/src/tpl.mts":         "const m = await import(`tpl-pad`)",
		"web/src/node.ts":         "import fs from 'node:fs'",
	}
	got := scanWeb(bad, `{"dependencies":{"react":"1","moment":"2"},"devDependencies":{"vite":"6"}}`)
	if r := rules(got); !slices.Equal(r, []string{"H5"}) {
		t.Fatalf("rules %v", r)
	}
	text := ""
	for _, v := range got {
		text += v.Msg + "\n"
	}
	for _, want := range []string{"lodash", "leaves web/src", "left-pad", "absolute path", "@scope/pkg", `"moment"`, "left-pad-js", "tpl-pad", "node built-ins"} {
		if !strings.Contains(text, want) {
			t.Errorf("no finding mentions %s:\n%s", want, text)
		}
	}
	good := map[string]string{
		"web/src/ok.ts":      "import { a } from './a'\nimport z from 'zustand'\nimport { it } from 'vitest'\nimport t from '@tanstack/react-query'",
		"web/src/ok.test.ts": "import fs from 'node:fs'",
		"web/src/f/g.tsx":    "import x from '../ok'\nexport * from '@ant-design/icons'",
	}
	if got := scanWeb(good, `{"devDependencies":{"typescript":"5","@biomejs/biome":"2"}}`); len(got) != 0 {
		t.Fatalf("legal web imports flagged: %v", got)
	}
	if got := scanWeb(nil, `{not json`); len(got) != 1 {
		t.Fatalf("invalid package.json not reported: %v", got)
	}
}

// writeTree materialises files under dir.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const goMod = "module " + module + "\n\ngo 1.26.0\n"

// TestSeededRepositoryGoesRedEndToEnd runs the real pipeline (go list, tree
// walk, every scanner) on a throw-away module seeded with one violation per
// rule, then on a clean module.
func TestSeededRepositoryGoesRedEndToEnd(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"go.mod": goMod,
		// D1: package in no layer
		"internal/misc/doc.go": "// Package misc is nobody's layer.\npackage misc\n",
		// P1: no doc comment
		"internal/audit/a.go": "package audit\n",
		// H0: contract imports a feature package
		"internal/contract/doc.go": "// Package contract is the kernel.\npackage contract\nimport _ \"" + module + "/internal/audit\"\n",
		// H1: tollgate internal
		"internal/dataplane/doc.go": "// Package dataplane wires tollgate.\npackage dataplane\nimport _ \"" + tollgate + "/internal/forward\"\n",
		// H2: store imports api
		"internal/store/doc.go": "// Package store is the base.\npackage store\nimport _ \"" + ap + "\"\n",
		"internal/api/doc.go":   "// Package api is the control plane.\npackage api\n",
		// H3: history imports store
		"internal/history/doc.go": "// Package history is pure.\npackage history\nimport _ \"" + st + "\"\n",
		// H4: tenancy writes calls
		"internal/tenancy/doc.go": "// Package tenancy owns orgs.\npackage tenancy\n",
		"internal/tenancy/q.sql":  "INSERT INTO calls (id) VALUES (1);\n",
		// H6: unlisted third party
		"internal/access/doc.go": "// Package access is RBAC.\npackage access\nimport _ \"github.com/gin-gonic/gin\"\n",
		// H7: build constraint and platform suffix
		"internal/config/doc.go":     "// Package config reads env.\npackage config\n",
		"internal/config/hidden.go":  "//go:build offline\n\npackage config\n",
		"internal/config/x_linux.go": "package config\n",
		// H8: tests without nonet.Run
		"internal/config/x_test.go": "package config\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n",
		// H9: nested module
		"internal/config/nested/go.mod": "module nested\n",
		// H5: web
		"web/src/a.ts":     "import _ from 'lodash'\n",
		"web/package.json": `{"dependencies":{"moment":"2"}}`,
	})
	got := rules(runOn(t, dir))
	want := append([]string(nil), allRules...)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("seeded repository fired %v, want every rule %v", got, want)
	}
}

func TestCleanRepositoryStaysGreenEndToEnd(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"go.mod":                   goMod,
		"internal/contract/doc.go": "// Package contract is the kernel.\npackage contract\n",
		"internal/store/doc.go":    "// Package store is the base.\npackage store\nimport _ \"" + cn + "\"\n",
		"internal/ledger/doc.go":   "// Package ledger writes.\npackage ledger\nimport _ \"" + st + "\"\n",
		"internal/ledger/w.go":     "package ledger\nconst q = `INSERT INTO calls (id) VALUES (1)`\n",
		"cmd/meterwised/main.go":   "// Command meterwised is the binary.\npackage main\nimport _ \"" + module + "/internal/ledger\"\nfunc main() {}\n",
		"web/src/ok.ts":            "import z from 'zustand'\n",
		"web/package.json":         `{"dependencies":{"zustand":"5"}}`,
	})
	if got := runOn(t, dir); len(got) != 0 {
		t.Fatalf("clean repository flagged: %v", got)
	}
}

func TestEveryRuleHasASeededViolation(t *testing.T) {
	// The end-to-end seed above must fire exactly allRules; this guards the
	// other direction: a rule ID used in code but missing from allRules.
	src, err := os.ReadFile("law_test.go")
	if err != nil {
		t.Fatal(err)
	}
	scan, err := os.ReadFile("scan_test.go")
	if err != nil {
		t.Fatal(err)
	}
	all := string(src) + string(scan)
	for _, id := range []string{"D1", "P1", "H0", "H1", "H2", "H3", "H4", "H5", "H6"} {
		if !slices.Contains(allRules, id) {
			t.Errorf("rule %s is missing from allRules", id)
		}
	}
	for _, line := range strings.Split(all, "\n") {
		for _, tag := range []string{`Violation{"`, `add("`} {
			if i := strings.Index(line, tag); i >= 0 {
				id := line[i+len(tag):]
				id = id[:strings.IndexByte(id, '"')]
				if strings.HasPrefix(id, "web/") || len(id) != 2 {
					continue
				}
				if !slices.Contains(allRules, id) {
					t.Errorf("rule %q is emitted but not in allRules (so it has no seed)", id)
				}
			}
		}
	}
}

func TestSeededBuildConstraintsGoRed(t *testing.T) {
	bad := map[string]string{
		"internal/savings/s.go":              "//go:build offline\n\npackage savings\n",
		"internal/savings/t.go":              "// +build integration\n\npackage savings\n",
		"internal/savings/u_linux.go":        "package savings\n",
		"internal/savings/v_arm64.go":        "package savings\n",
		"internal/savings/w_windows_test.go": "package savings\n",
	}
	for name, body := range bad {
		if got := scanBuildConstraints(map[string]string{name: body}); len(got) == 0 || got[0].Rule != "H7" {
			t.Errorf("%s not flagged: %v", name, got)
		}
	}
	good := map[string]string{
		"internal/savings/s.go":           "// Package savings.\n// The go:build text in a comment line is prose.\npackage savings\n",
		"internal/savings/linux_notes.go": "package savings\n",
		"internal/savings/s_test.go":      "package savings\n",
		"internal/savings/price_table.go": "package savings\n",
	}
	if got := scanBuildConstraints(good); len(got) != 0 {
		t.Fatalf("legal files flagged: %v", got)
	}
}

func TestSeededMissingTestMainGoesRed(t *testing.T) {
	const guarded = "package p\nimport (\"os\"; \"testing\"; \"github.com/ethosagent/meterwise/internal/nonet\")\nfunc TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }\n"
	bad := map[string]string{
		"internal/config/a_test.go": "package p\nfunc TestA(t *testing.T) {}\n",
		"internal/store/b_test.go":  "package p\nfunc TestMain(m *testing.M) { os.Exit(m.Run()) }\n",
		// the name only appears in a comment, a string, or an unrelated function: not a call
		"internal/api/c_test.go":    "package p\nimport (\"os\"; \"testing\"; \"github.com/ethosagent/meterwise/internal/nonet\")\n// TestMain would call nonet.Run(m) here\nfunc TestMain(m *testing.M) { _ = \"nonet.Run(m)\"; os.Exit(m.Run()) }\nfunc helper(m *testing.M) int { return nonet.Run(m) }\n",
		"internal/ledger/d_test.go": "package p\nimport (\"os\"; \"testing\")\nfunc TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }\n",
	}
	got := scanTestMain(bad)
	if len(got) != 4 || got[0].Rule != "H8" {
		t.Fatalf("want four H8 findings, got %v", got)
	}
	good := map[string]string{
		"internal/config/a_test.go":    "package p\nfunc TestA(t *testing.T) {}\n",
		"internal/config/main_test.go": guarded,
		"internal/archtest/a_test.go":  "package p\n",
		"internal/api/doc.go":          "package api\n",
	}
	if got := scanTestMain(good); len(got) != 0 {
		t.Fatalf("guarded or exempt packages flagged: %v", got)
	}
}

func TestSeededDistDirIsStillScanned(t *testing.T) {
	// a package under a directory named dist or testdata-like names is scanned; only web/dist is skipped
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"go.mod":                       goMod,
		"internal/savings/dist/doc.go": "// Package dist hides behind a directory name.\npackage dist\n",
		"internal/savings/dist/x.go":   "//go:build offline\n\npackage dist\n",
		"web/dist/ignored.js":          "import x from 'left-pad'\n",
	})
	got := runOn(t, dir)
	r := rules(got)
	if !slices.Contains(r, "H7") {
		t.Fatalf("a build tag under internal/savings/dist must be found, got %v", r)
	}
	for _, v := range got {
		if strings.Contains(v.Msg, "web/dist") {
			t.Fatalf("web/dist must stay skipped: %v", v)
		}
	}
}

func TestSeededNestedModuleGoesRed(t *testing.T) {
	bad := map[string]string{
		"internal/config/nested/go.mod": "module x\n",
		"cmd/x/go.mod":                  "module y\n",
		"go.work":                       "go 1.26\n",
		"tools/go.work":                 "go 1.26\n",
	}
	if got := scanModules(bad); len(got) != 4 || got[0].Rule != "H9" {
		t.Fatalf("want four H9 findings, got %v", got)
	}
	if got := scanModules(map[string]string{"go.mod": goMod, "tools/go.mod": "module t\n"}); len(got) != 0 {
		t.Fatalf("root and tools modules are allowed: %v", got)
	}
}

func TestSeededAliasedUpdateGoesRed(t *testing.T) {
	for _, body := range []string{"UPDATE calls c SET n = 1", "UPDATE sessions AS s SET n = 1", "UPDATE ONLY public.\"sessions\" s SET n = 1"} {
		if got := scanWriters(map[string]string{"internal/tenancy/a.sql": body}); len(got) != 1 {
			t.Errorf("%q not flagged: %v", body, got)
		}
	}
}
