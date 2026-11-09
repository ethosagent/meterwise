package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const migrationsDir = "internal/store/migrations"

// target is one generated file.
type target struct {
	name string
	path string // repo-relative
	gen  func(root string) ([]byte, error)
}

func targets() []target {
	return []target{
		{"contract-ts", "web/src/contract/generated.ts", func(string) ([]byte, error) { return genContractTS() }},
		{"openapi", "internal/api/openapi.json", func(string) ([]byte, error) { return genOpenAPI() }},
		{"schema", "docs/schema.md", func(root string) ([]byte, error) {
			ms, _, err := loadMigrations(os.DirFS(root), migrationsDir)
			if err != nil {
				return nil, err
			}
			return renderSchema(ms), nil
		}},
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	check := fs.Bool("check", false, "fail if a generated file differs from what would be written")
	root := fs.String("root", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	what := "all"
	if fs.NArg() > 0 {
		what = fs.Arg(0)
	}
	var failed bool
	ran := false
	if what == "all" || what == "migrations" {
		ran = true
		failed = runLint(*root, stdout, stderr) || failed
	}
	for _, t := range targets() {
		if what != "all" && what != t.name {
			continue
		}
		ran = true
		failed = runTarget(*root, t, *check, stdout, stderr) || failed
	}
	if !ran {
		fmt.Fprintf(stderr, "gen: unknown target %q (all|contract-ts|openapi|schema|migrations)\n", what)
		return 2
	}
	if failed {
		return 1
	}
	return 0
}

func runLint(root string, stdout, stderr io.Writer) (failed bool) {
	ms, stray, err := loadMigrations(os.DirFS(root), migrationsDir)
	if err != nil {
		fmt.Fprintln(stderr, "✗", err)
		return true
	}
	if len(ms) == 0 && len(stray) == 0 {
		fmt.Fprintln(stdout, "SKIP migrations lint: internal/store/migrations has no migrations yet (goes live with the first one)")
		return false
	}
	families, famProblems, err := loadFamilies(os.DirFS(root))
	if err != nil {
		fmt.Fprintln(stderr, "✗", err)
		return true
	}
	for _, p := range famProblems {
		fmt.Fprintln(stderr, "✗", p)
		failed = true
	}
	for _, p := range lintMigrations(ms, stray, families) {
		fmt.Fprintln(stderr, "✗", p)
		failed = true
	}
	return failed
}

func runTarget(root string, t target, check bool, stdout, stderr io.Writer) (failed bool) {
	want, err := t.gen(root)
	if err != nil {
		fmt.Fprintf(stderr, "✗ %s: %v\n", t.name, err)
		return true
	}
	rt, err := os.OpenRoot(root)
	if err != nil {
		fmt.Fprintf(stderr, "✗ %s: %v\n", t.path, err)
		return true
	}
	defer rt.Close()
	have, readErr := rt.ReadFile(t.path)
	if check {
		switch {
		case readErr != nil:
			fmt.Fprintf(stderr, "✗ %s is missing. Fix: go run ./cmd/gen\n", t.path)
			return true
		case !bytes.Equal(have, want):
			fmt.Fprintf(stderr, "✗ %s is stale or was edited by hand (first difference at line %d). Fix: go run ./cmd/gen\n", t.path, firstDiffLine(have, want))
			return true
		}
		return false
	}
	if readErr == nil && bytes.Equal(have, want) {
		return false
	}
	if err := rt.MkdirAll(filepath.ToSlash(filepath.Dir(t.path)), 0o750); err != nil {
		fmt.Fprintf(stderr, "✗ %s: %v\n", t.path, err)
		return true
	}
	if err := rt.WriteFile(t.path, want, 0o600); err != nil {
		fmt.Fprintf(stderr, "✗ %s: %v\n", t.path, err)
		return true
	}
	fmt.Fprintf(stdout, "wrote %s\n", t.path)
	return false
}

func firstDiffLine(a, b []byte) int {
	al, bl := strings.Split(string(a), "\n"), strings.Split(string(b), "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return i + 1
		}
	}
	return min(len(al), len(bl)) + 1
}
