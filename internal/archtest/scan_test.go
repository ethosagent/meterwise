package archtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
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

var writesLedger = regexp.MustCompile(`(?i)\b(INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+(public\.)?(calls|sessions|monitor_events|audit_log)\b`)

// scanWriters applies H4 to files (repo-relative path -> content).
func scanWriters(files map[string]string) []Violation {
	var out []Violation
	for rel, body := range files {
		ext := path.Ext(rel)
		if (ext != ".go" && ext != ".sql") || strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, "internal/archtest/") {
			continue
		}
		if contains(writerFiles, rel) || underDirs(rel, writerDirs) {
			continue
		}
		if m := writesLedger.FindString(body); m != "" {
			out = append(out, Violation{"H4", fmt.Sprintf("%s writes a ledger table (%q); only internal/ledger and internal/audit own calls, sessions, monitor_events, audit_log", rel, m)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Msg < out[j].Msg })
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
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
	"@tanstack/react-query", "zustand", "recharts",
	"@types/react", "@types/react-dom", "@types/node", "@vitejs/plugin-react",
	"vite", "vitest", "@vitest/coverage-v8", "typescript", "@biomejs/biome", "jsdom",
	"@testing-library/react", "@testing-library/jest-dom", "@testing-library/user-event",
}

var importRe = regexp.MustCompile(`(?m)(?:\bfrom\s*|\bimport\s*\(?\s*|\brequire\(\s*)["']([^"']+)["']`)

// scanWeb applies H5 to web sources (repo-relative path -> content) and to
// web/package.json (pkgJSON, "" when absent).
func scanWeb(files map[string]string, pkgJSON string) []Violation {
	var out []Violation
	add := func(msg string) { out = append(out, Violation{"H5", msg}) }
	for rel, body := range files {
		isTS := strings.HasSuffix(rel, ".ts") || strings.HasSuffix(rel, ".tsx")
		if !strings.HasPrefix(rel, "web/src/") || !isTS {
			continue
		}
		for _, m := range importRe.FindAllStringSubmatch(body, -1) {
			if msg := checkWebImport(rel, m[1]); msg != "" {
				add(msg)
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
				if !contains(webAllow, name) {
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
		return ""
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
	if !contains(webAllow, name) {
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
	return out
}

func listPackages(t *testing.T, dir string) []pkg {
	t.Helper()
	cmd := exec.Command("go", "list", "-e", "-json", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
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
			if name == ".git" || name == "node_modules" || name == "dist" || name == ".logs" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		switch path.Ext(name) {
		case ".go", ".sql", ".ts", ".tsx":
		default:
			if name != "package.json" {
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
