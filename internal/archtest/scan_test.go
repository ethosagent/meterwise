package archtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// writerFiles may contain SQL that writes the ledger tables (H4): the sqlc
// query files and generated output of the two owning features.
var writerFiles = []string{
	"internal/store/queries/ledger.sql", "internal/store/queries/audit.sql",
	"internal/store/sqlc/ledger.sql.go", "internal/store/sqlc/audit.sql.go",
}

// writerDirs are the packages that own those tables (H4), non-test files only.
var writerDirs = []string{"internal/ledger", "internal/audit"}

// ident matches a possibly quoted, possibly schema-qualified table name.
const ident = `(?:"?public"?\.)?"?`

// ledgerTables reads contract.LedgerTables from its source (archtest depends on the
// standard library only, so it parses the file instead of importing the package).
// go test runs in this package's directory.
var ledgerTables = func() []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "../contract/ledger.go", nil, 0)
	if err != nil {
		panic("archtest: cannot read the ledger table list: " + err.Error())
	}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range gd.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "LedgerTables" || len(vs.Values) != 1 {
				continue
			}
			lit, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok {
				continue
			}
			var out []string
			for _, e := range lit.Elts {
				bl, ok := e.(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					panic("archtest: contract.LedgerTables must be a list of string literals")
				}
				name, err := strconv.Unquote(bl.Value)
				if err != nil {
					panic("archtest: contract.LedgerTables: " + err.Error())
				}
				out = append(out, regexp.QuoteMeta(name))
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	panic("archtest: contract.LedgerTables not found or empty in internal/contract/ledger.go")
}()

var ledgerAlt = strings.Join(ledgerTables, "|")

var writesLedger = regexp.MustCompile(`(?i)\b(INSERT\s+INTO|DELETE\s+FROM(?:\s+ONLY)?|MERGE\s+INTO(?:\s+ONLY)?|TRUNCATE(?:\s+TABLE)?(?:\s+ONLY)?|COPY)\s+` + ident + `(?:` + ledgerAlt + `)"?(?:[^a-z0-9_]|$)`)

var writesLedgerUpdate = regexp.MustCompile(`(?i)\bUPDATE\s+(?:ONLY\s+)?` + ident + `(?:` + ledgerAlt + `)"?(?:\s+(?:AS\s+)?[a-z_][a-z0-9_]*)?\s+SET\b`)

var (
	goComments  = regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*`)
	sqlComments = regexp.MustCompile(`(?s)/\*.*?\*/|--[^\n]*`)
)

// scanWriters applies H4 to files (repo-relative path -> content). Comments are
// stripped first so prose such as "update sessions" never trips the rule.
func scanWriters(files map[string]string) []Violation {
	var out []Violation
	for rel, body := range files {
		ext := path.Ext(rel)
		if (ext != ".go" && ext != ".sql") || strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, "internal/archtest/") || inTestdata(rel) {
			continue
		}
		if slices.Contains(writerFiles, rel) || underDirs(rel, writerDirs) {
			continue
		}
		strip := sqlComments
		if ext == ".go" {
			strip = goComments
		}
		code := strip.ReplaceAllString(body, " ")
		m := writesLedger.FindString(code)
		if m == "" {
			m = writesLedgerUpdate.FindString(code)
		}
		if m != "" {
			out = append(out, Violation{"H4", fmt.Sprintf("%s writes a ledger table (%q); only internal/ledger and internal/audit own the ledger tables (contract.LedgerTables)", rel, strings.Join(strings.Fields(m), " "))})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Msg < out[j].Msg })
	return out
}

func underDirs(rel string, dirs []string) bool {
	for _, d := range dirs {
		if strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

// webAllow is rule H5's npm allowlist (docs/decisions/0007): the decision D2
// stack plus the pinned dev toolchain. Adding a package needs an ADR.
var webAllow = []string{
	"react", "react-dom", "react-router", "react-router-dom", "antd", "@ant-design/icons",
	"@tanstack/react-query", "zustand", "recharts", "@clerk/react",
	"@types/react", "@types/react-dom", "@types/node", "@vitejs/plugin-react",
	"vite", "vitest", "@vitest/coverage-v8", "typescript", "@biomejs/biome", "jsdom",
	"@testing-library/react", "@testing-library/dom", "@testing-library/jest-dom", "@testing-library/user-event",
}

// importRes match only real module specifiers, each with the specifier as
// group 1. A bare `from "x"` is not enough: string text such as
// `{ name: "Where it came from" }` must not read as an import.
//
//  1. a static statement at a line start or after `;`, `{` or `}`: `import ... from "x"` or
//     `export ... from "x"`. The clause between the keyword and `from` is
//     limited to identifiers, braces, commas, `*` and whitespace (so it spans
//     lines but never a quote, `;` or `=`), plus line and block comments.
//  2. a side-effect `import "x"` at the same anchors.
//  3. a dynamic `import("x")`.
//  4. `require("x")` (also covers `import x = require("x")`).
//
// Known limit: string-named members (`import { "a-b" as ab } from "x"`) are not matched.
var importRes = []*regexp.Regexp{
	regexp.MustCompile("(?m)(?:^|[;{}])[ \\t]*(?:import|export)\\b(?:[\\w$\\s{},*]|//[^\\n]*\\n|/\\*(?:[^*]|\\*+[^*/])*\\*+/)*?\\bfrom\\s*[\"'`]([^\"'`]+)[\"'`]"),
	regexp.MustCompile("(?m)(?:^|[;{}])[ \\t]*import\\s*[\"'`]([^\"'`]+)[\"'`]"),
	regexp.MustCompile("\\bimport\\s*\\(\\s*[\"'`]([^\"'`]+)[\"'`]"),
	regexp.MustCompile("\\brequire\\s*\\(\\s*[\"'`]([^\"'`]+)[\"'`]"),
}

var webSource = regexp.MustCompile(`\.(ts|tsx|js|jsx|mjs|cjs|mts|cts)$`)

// scanWeb applies H5 to web sources (repo-relative path -> content) and to
// web/package.json (pkgJSON, "" when absent).
func scanWeb(files map[string]string, pkgJSON string) []Violation {
	var out []Violation
	add := func(msg string) { out = append(out, Violation{"H5", msg}) }
	for rel, body := range files {
		if !strings.HasPrefix(rel, "web/src/") || !webSource.MatchString(rel) {
			continue
		}
		for _, re := range importRes {
			for _, m := range re.FindAllStringSubmatch(body, -1) {
				if msg := checkWebImport(rel, m[1]); msg != "" {
					add(msg)
				}
			}
		}
	}
	if pkgJSON != "" {
		var p struct {
			Dependencies, DevDependencies, PeerDependencies, OptionalDependencies map[string]string
		}
		if err := json.Unmarshal([]byte(pkgJSON), &p); err != nil {
			add("web/package.json is not valid JSON: " + err.Error())
		}
		for _, set := range []map[string]string{p.Dependencies, p.DevDependencies, p.PeerDependencies, p.OptionalDependencies} {
			for name := range set {
				if !slices.Contains(webAllow, name) {
					add(fmt.Sprintf("web/package.json declares %q: not on the npm allowlist (needs an ADR and an entry in law_test.go)", name))
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Msg < out[j].Msg })
	return out
}

func checkWebImport(rel, spec string) string {
	switch {
	case strings.HasPrefix(spec, "node:"):
		if strings.Contains(rel, ".test.") {
			return ""
		}
		return fmt.Sprintf("%s imports %q: node built-ins are for tests only; web/src runs in the browser", rel, spec)
	case strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../"):
		target := path.Join(path.Dir(rel), spec)
		if !strings.HasPrefix(target+"/", "web/src/") {
			return fmt.Sprintf("%s imports %q, which leaves web/src/", rel, spec)
		}
		return ""
	case strings.HasPrefix(spec, "/"):
		return fmt.Sprintf("%s imports absolute path %q; web/ imports only web/src/** and allowlisted packages", rel, spec)
	}
	var name string
	parts := strings.Split(spec, "/")
	if strings.HasPrefix(spec, "@") && len(parts) >= 2 {
		name = parts[0] + "/" + parts[1]
	} else {
		name = parts[0]
	}
	if !slices.Contains(webAllow, name) {
		return fmt.Sprintf("%s imports %q: package %q is not on the npm allowlist (needs an ADR and an entry in law_test.go)", rel, spec, name)
	}
	return ""
}

// goListPkg mirrors the fields of `go list -json` the law uses.
type goListPkg struct {
	ImportPath   string
	Doc          string
	Imports      []string
	TestImports  []string
	XTestImports []string
}

// runOn applies every rule to the repository rooted at dir.
func runOn(t *testing.T, dir string) []Violation {
	t.Helper()
	pkgs := listPackages(t, dir)
	if len(pkgs) == 0 {
		t.Fatal("go list returned no packages: the gate would be vacuous")
	}
	files := readTree(t, dir)
	out := analyze(pkgs)
	out = append(out, scanWriters(files)...)
	out = append(out, scanWeb(files, files["web/package.json"])...)
	out = append(out, scanBuildConstraints(files)...)
	out = append(out, scanTestMain(files)...)
	out = append(out, scanModules(files)...)
	out = append(out, scanInlineRoleChecks(files)...)
	return out
}

func listPackages(t *testing.T, dir string) []pkg {
	t.Helper()
	cmd := exec.Command("go", "list", "-e", "-json", "./...")
	cmd.Dir = dir
	// readonly: the gate must never download modules or rewrite go.mod/go.sum
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=readonly")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list in %s: %v\n%s", dir, err, stderr.String())
	}
	var pkgs []pkg
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p goListPkg
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			return pkgs
		} else if err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		rel := "."
		if p.ImportPath != module {
			rel = strings.TrimPrefix(p.ImportPath, module+"/")
		}
		pkgs = append(pkgs, pkg{Rel: rel, Doc: p.Doc, Imports: p.Imports, TestImports: append(p.TestImports, p.XTestImports...)})
	}
}

// readTree reads every .go, .sql, .ts, .tsx and package.json under dir that a
// rule scans, keyed by slash-separated path relative to dir.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			// Only these are skipped: VCS data, installed packages, gate logs and the
			// web build output at its one real location. A dist/ or testdata/ elsewhere is scanned.
			relDir, relErr := filepath.Rel(dir, p)
			if relErr != nil {
				return relErr
			}
			relDir = filepath.ToSlash(relDir)
			if name == ".git" || name == "node_modules" || name == ".logs" || relDir == "web/dist" || relDir == "web/coverage" {
				return filepath.SkipDir
			}
			return nil
		}
		switch path.Ext(name) {
		case ".go", ".sql", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts":
		default:
			if name != "package.json" && name != "go.mod" && name != "go.work" {
				return nil
			}
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return files
}

var knownOS = []string{"aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "js", "linux", "nacl", "netbsd", "openbsd", "plan9", "solaris", "wasip1", "windows", "zos"}
var knownArch = []string{"386", "amd64", "amd64p32", "arm", "armbe", "arm64", "arm64be", "loong64", "mips", "mipsle", "mips64", "mips64le", "mips64p32", "mips64p32le", "ppc", "ppc64", "ppc64le", "riscv", "riscv64", "s390", "s390x", "sparc", "sparc64", "wasm"}

// buildConstraintAllow lists Go files (repo-relative) that may carry a build
// constraint or a GOOS/GOARCH suffix. Empty on purpose: a constraint can hide
// code from every gate (the law, vet, lint, tests, coverage), so each one needs
// an ADR and an entry here.
var buildConstraintAllow = []string{}

var buildLine = regexp.MustCompile(`(?m)^[ \t]*//(?:go:build|[ \t]*\+build)\b`)

// scanBuildConstraints applies H7: no //go:build, // +build or GOOS/GOARCH
// filename suffix in a Go file unless allowlisted.
func scanBuildConstraints(files map[string]string) []Violation {
	var out []Violation
	for rel, body := range files {
		if path.Ext(rel) != ".go" || slices.Contains(buildConstraintAllow, rel) || inTestdata(rel) {
			continue
		}
		if buildLine.MatchString(body) {
			out = append(out, Violation{"H7", rel + " has a build constraint; it can hide code from the gates (law, vet, lint, test, coverage). Remove it or allowlist the file in law_test.go with an ADR"})
		}
		if hasPlatformSuffix(strings.TrimSuffix(path.Base(rel), ".go")) {
			out = append(out, Violation{"H7", rel + " has a GOOS/GOARCH filename suffix, which is an implicit build constraint; rename it or allowlist it with an ADR"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Msg < out[j].Msg })
	return out
}

func hasPlatformSuffix(base string) bool {
	base = strings.TrimSuffix(base, "_test")
	parts := strings.Split(base, "_")
	if len(parts) < 2 {
		return false
	}
	last := parts[len(parts)-1]
	if slices.Contains(knownArch, last) {
		return true
	}
	return slices.Contains(knownOS, last)
}

// testMainExempt packages may have tests without nonet.Run: archtest shells out
// to `go list` and reads files only.
var testMainExempt = []string{"internal/archtest"}

const nonetPath = module + "/internal/nonet"

// inTestdata reports a path the go tool ignores (a testdata directory).
func inTestdata(rel string) bool {
	return strings.HasPrefix(rel, "testdata/") || strings.Contains(rel, "/testdata/")
}

// callsNonetRun reports whether a test file declares func TestMain and its body
// really calls nonet.Run (parsed, so a comment or a string never counts).
func callsNonetRun(rel, body string) bool {
	f, err := parser.ParseFile(token.NewFileSet(), rel, body, 0)
	if err != nil {
		return false
	}
	local := ""
	for _, imp := range f.Imports {
		if strings.Trim(imp.Path.Value, "\"`") != nonetPath {
			continue
		}
		local = "nonet"
		if imp.Name != nil {
			local = imp.Name.Name
		}
	}
	if local == "" || local == "_" || local == "." {
		return false
	}
	found := false
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "TestMain" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Run" {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == local {
					found = true
				}
			}
			return true
		})
	}
	return found
}

// scanTestMain applies H8: every directory with a _test.go file has a TestMain
// that calls nonet.Run, so no test can reach the external network by name.
func scanTestMain(files map[string]string) []Violation {
	hasTests := map[string]bool{}
	guarded := map[string]bool{}
	for rel, body := range files {
		if !strings.HasSuffix(rel, "_test.go") || inTestdata(rel) {
			continue
		}
		dir := path.Dir(rel)
		hasTests[dir] = true
		if callsNonetRun(rel, body) {
			guarded[dir] = true
		}
	}
	var out []Violation
	for dir := range hasTests {
		if !guarded[dir] && !slices.Contains(testMainExempt, dir) {
			out = append(out, Violation{"H8", dir + " has tests but no TestMain that calls nonet.Run; without it a test can reach the internet and stay green"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Msg < out[j].Msg })
	return out
}

// moduleAllow lists the nested modules allowed besides the root go.mod. A nested
// module is invisible to `go test ./...` and to go list of the root, so it could
// hide code from every gate; tools/ is the pinned dev-tool module (ADR 0005).
var moduleAllow = []string{"tools/go.mod"}

// scanModules applies H9: one module, plus the allowlisted tools module; no go.work.
func scanModules(files map[string]string) []Violation {
	var out []Violation
	for rel := range files {
		switch path.Base(rel) {
		case "go.mod":
			if rel != "go.mod" && !slices.Contains(moduleAllow, rel) {
				out = append(out, Violation{"H9", rel + " is a nested module: its packages are invisible to the root gates. Remove it or allowlist it in law_test.go with an ADR"})
			}
		case "go.work":
			out = append(out, Violation{"H9", rel + ": a go.work changes what every gate builds; it is not allowed"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Msg < out[j].Msg })
	return out
}
