package main

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

// column is one column definition as written in the migration.
type column struct {
	Name, Def, Migration string
}

type table struct {
	Name, Feature, Migration string
	Columns                  []column
}

type migration struct {
	File, Feature string
	headerFeature string
	HasDown       bool
	Created       []string
	Touched       []string
	tables        []table
	adds          map[string][]column
}

var (
	fileRe    = regexp.MustCompile(`^(\d{4})_([a-z][a-z0-9_]*)\.sql$`)
	featureRe = regexp.MustCompile(`(?m)^--\s*feature:\s*([a-z][a-z0-9_]*)\s*$`)
	createRe  = regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:public\.)?([a-z_][a-z0-9_]*)\s*\(`)
	addColRe  = regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(?:ONLY\s+)?(?:public\.)?([a-z_][a-z0-9_]*)\s+ADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)\s+([^;,]+)`)
	touchRes  = []*regexp.Regexp{
		regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(?:ONLY\s+)?(?:public\.)?([a-z_][a-z0-9_]*)`),
		regexp.MustCompile(`(?is)DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:public\.)?([a-z_][a-z0-9_]*)`),
		regexp.MustCompile(`(?is)CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?\S+\s+ON\s+(?:ONLY\s+)?(?:public\.)?([a-z_][a-z0-9_]*)`),
		regexp.MustCompile(`(?is)INSERT\s+INTO\s+(?:public\.)?([a-z_][a-z0-9_]*)`),
		regexp.MustCompile(`(?is)(?:^|[;\s])UPDATE\s+(?:ONLY\s+)?(?:public\.)?([a-z_][a-z0-9_]*)\s+SET`),
		regexp.MustCompile(`(?is)DELETE\s+FROM\s+(?:ONLY\s+)?(?:public\.)?([a-z_][a-z0-9_]*)`),
		regexp.MustCompile(`(?is)TRUNCATE\s+(?:TABLE\s+)?(?:ONLY\s+)?(?:public\.)?([a-z_][a-z0-9_]*)`),
	}
	lineComment = regexp.MustCompile(`(?m)--[^\n]*`)
)

// splitGoose returns the Up and Down halves of a goose migration.
func splitGoose(sql string) (up, down string) {
	i := strings.Index(sql, "-- +goose Down")
	if i < 0 {
		return sql, ""
	}
	return sql[:i], sql[i:]
}

// topLevelCommas splits s at commas that are not inside parentheses.
func topLevelCommas(s string) []string {
	var parts []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

// balanced returns the text inside the parentheses that open at s[open].
func balanced(s string, open int) (string, bool) {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[open+1 : i], true
			}
		}
	}
	return "", false
}

var constraintStart = regexp.MustCompile(`(?i)^(primary|unique|foreign|check|constraint|exclude|like)\b`)

func parseColumns(body, file string) []column {
	var cols []column
	for _, p := range topLevelCommas(body) {
		p = strings.Join(strings.Fields(p), " ")
		if p == "" || constraintStart.MatchString(p) {
			continue
		}
		name, def, _ := strings.Cut(p, " ")
		cols = append(cols, column{Name: strings.Trim(name, `"`), Def: def, Migration: file})
	}
	return cols
}

func parseMigration(file, sql string) migration {
	m := migration{File: file, adds: map[string][]column{}}
	if f := fileRe.FindStringSubmatch(file); f != nil {
		m.Feature = f[2]
	}
	upRaw, down := splitGoose(sql)
	m.HasDown = strings.TrimSpace(lineComment.ReplaceAllString(down, "")) != ""
	up := lineComment.ReplaceAllString(upRaw, "")
	for _, loc := range createRe.FindAllStringSubmatchIndex(up, -1) {
		name := up[loc[2]:loc[3]]
		body, ok := balanced(up, loc[1]-1)
		if !ok {
			continue
		}
		m.Created = append(m.Created, name)
		m.tables = append(m.tables, table{Name: name, Feature: m.Feature, Migration: file, Columns: parseColumns(body, file)})
	}
	for _, a := range addColRe.FindAllStringSubmatch(up, -1) {
		def := strings.Join(strings.Fields(a[3]), " ")
		m.adds[a[1]] = append(m.adds[a[1]], column{Name: a[2], Def: def, Migration: file})
	}
	touched := map[string]bool{}
	for _, n := range m.Created {
		touched[n] = true
	}
	for _, re := range touchRes {
		for _, t := range re.FindAllStringSubmatch(up, -1) {
			touched[t[1]] = true
		}
	}
	for n := range touched {
		m.Touched = append(m.Touched, n)
	}
	sort.Strings(m.Touched)
	return m
}

// loadMigrations reads the migrations in dir (a path relative to fsys); a
// missing directory means no migrations yet.
func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("gen: read %s: %w", dir, err)
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("gen: read migration: %w", err)
		}
		out = append(out, parseMigration(e.Name(), string(b)))
		if f := featureRe.FindSubmatch(b); f != nil {
			out[len(out)-1].headerFeature = string(f[1])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out, nil
}
