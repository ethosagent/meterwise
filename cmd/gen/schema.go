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

// id matches a table name that may be quoted and schema-qualified; group 1 is the bare name.
const id = `(?:"?[a-z_][a-z0-9_]*"?\.)?"?([a-z_][a-z0-9_]*)"?`

var (
	fileRe    = regexp.MustCompile(`^(\d{4})_([a-z][a-z0-9_]*)\.sql$`)
	featureRe = regexp.MustCompile(`(?m)^--\s*feature:\s*([a-z][a-z0-9_]*)\s*$`)
	createRe  = regexp.MustCompile(`(?is)CREATE\s+(?:UNLOGGED\s+|TEMP(?:ORARY)?\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?` + id)
	viewRe    = regexp.MustCompile(`(?is)CREATE\s+(?:MATERIALIZED\s+)?VIEW\s+(?:IF\s+NOT\s+EXISTS\s+)?` + id)
	addColRe  = regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?` + id + `\s+ADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?"?([a-z_][a-z0-9_]*)"?\s+([^;,]+)`)
	touchRes  = []*regexp.Regexp{
		regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?` + id),
		regexp.MustCompile(`(?is)DROP\s+(?:MATERIALIZED\s+)?(?:TABLE|VIEW)\s+(?:IF\s+EXISTS\s+)?` + id),
		regexp.MustCompile(`(?is)CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?(?:\S+\s+)?ON\s+(?:ONLY\s+)?` + id),
		regexp.MustCompile(`(?is)INSERT\s+INTO\s+` + id),
		regexp.MustCompile(`(?is)(?:^|[;\s])UPDATE\s+(?:ONLY\s+)?` + id + `(?:\s+(?:AS\s+)?[a-z_][a-z0-9_]*)?\s+SET\b`),
		regexp.MustCompile(`(?is)(?:^|[;\s])COPY\s+` + id + `\s*(?:\(|FROM\b)`),
		regexp.MustCompile(`(?is)DELETE\s+FROM\s+(?:ONLY\s+)?` + id),
		regexp.MustCompile(`(?is)MERGE\s+INTO\s+(?:ONLY\s+)?` + id),
		regexp.MustCompile(`(?is)TRUNCATE\s+(?:TABLE\s+)?(?:ONLY\s+)?` + id),
		regexp.MustCompile(`(?is)CREATE\s+(?:OR\s+REPLACE\s+)?(?:CONSTRAINT\s+)?TRIGGER\s+[^;]*?\sON\s+` + id),
		regexp.MustCompile(`(?is)DROP\s+TRIGGER\s+(?:IF\s+EXISTS\s+)?\S+\s+ON\s+` + id),
		regexp.MustCompile(`(?is)(?:CREATE|ALTER|DROP)\s+POLICY\s+(?:IF\s+EXISTS\s+)?\S+\s+ON\s+` + id),
		regexp.MustCompile(`(?is)CREATE\s+(?:OR\s+REPLACE\s+)?RULE\s+[^;]*?\sTO\s+` + id),
		regexp.MustCompile(`(?is)COMMENT\s+ON\s+(?:TABLE|VIEW|MATERIALIZED\s+VIEW)\s+` + id),
		regexp.MustCompile(`(?is)COMMENT\s+ON\s+COLUMN\s+(?:[a-z_"][a-z0-9_"]*\.)?"?([a-z_][a-z0-9_]*)"?\."?[a-z_][a-z0-9_]*"?\s+IS`),
		regexp.MustCompile(`(?is)(?:GRANT|REVOKE)\s+[^;]*?\sON\s+TABLE\s+` + id),
		regexp.MustCompile(`(?is)(?:GRANT|REVOKE)\s+[^;]*?\sON\s+` + id + `\s+(?:TO|FROM)\b`),
		regexp.MustCompile(`(?is)PARTITION\s+OF\s+` + id),
		regexp.MustCompile(`(?is)CREATE\s+OR\s+REPLACE\s+VIEW\s+` + id),
	}
	lineComment = regexp.MustCompile(`(?s)/\*.*?\*/|--[^\n]*`)
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
	m.HasDown = strings.TrimSpace(lineComment.ReplaceAllString(down, " ")) != ""
	up := lineComment.ReplaceAllString(upRaw, " ")
	for _, loc := range createRe.FindAllStringSubmatchIndex(up, -1) {
		name := up[loc[2]:loc[3]]
		m.Created = append(m.Created, name)
		t := table{Name: name, Feature: m.Feature, Migration: file}
		if rest := strings.TrimLeft(up[loc[1]:], " \t\r\n"); strings.HasPrefix(rest, "(") {
			if body, ok := balanced(up, loc[1]+(len(up[loc[1]:])-len(rest))); ok {
				t.Columns = parseColumns(body, file)
			}
		}
		m.tables = append(m.tables, t)
	}
	for _, loc := range viewRe.FindAllStringSubmatchIndex(up, -1) {
		name := up[loc[2]:loc[3]]
		m.Created = append(m.Created, name)
		m.tables = append(m.tables, table{Name: name, Feature: m.Feature, Migration: file})
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
			// `TRUNCATE ON <table>` (a trigger event or a REVOKE) is not a TRUNCATE of a table
			// called "on": ON is reserved, so it is never a table name, and the statement's real
			// target is caught by the ON patterns.
			if !strings.EqualFold(t[1], "on") {
				touched[t[1]] = true
			}
		}
	}
	for n := range touched {
		m.Touched = append(m.Touched, n)
	}
	sort.Strings(m.Touched)
	return m
}

// loadMigrations reads the migrations in dir (a path relative to fsys); a
// missing directory means no migrations yet. stray lists entries that are not
// migrations (subdirectories, upper-case .SQL, other files): the gate refuses
// them rather than silently ignoring SQL that goose would not run either.
func loadMigrations(fsys fs.FS, dir string) (out []migration, stray []string, err error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("gen: read %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			stray = append(stray, path.Join(dir, e.Name()))
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, nil, fmt.Errorf("gen: read migration: %w", err)
		}
		out = append(out, parseMigration(e.Name(), string(b)))
		if f := featureRe.FindSubmatch(b); f != nil {
			out[len(out)-1].headerFeature = string(f[1])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out, stray, nil
}

// loadFamilies reads internal/store/migration-families.txt: lines of
// `<feature> <table> [<table>...]` naming tables a feature owns although the
// table name does not start with the feature's name (for example the ledger
// feature owning calls and sessions). A protected path. problems lists garbage
// lines and tables listed twice; lintFamilies checks the rest against the migrations.
func loadFamilies(fsys fs.FS) (fam map[string][]string, problems []string, err error) {
	b, err := fs.ReadFile(fsys, familiesFile)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("gen: read %s: %w", familiesFile, err)
	}
	fam = map[string][]string{}
	listed := map[string]string{}
	for n, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(strings.SplitN(line, "#", 2)[0])
		if len(f) == 0 {
			continue
		}
		if len(f) < 2 || !identRe.MatchString(f[0]) {
			problems = append(problems, fmt.Sprintf("%s:%d: want `<feature> <table> [<table>...]`, got %q", familiesFile, n+1, strings.TrimSpace(line)))
			continue
		}
		for _, t := range f[1:] {
			switch {
			case !identRe.MatchString(t):
				problems = append(problems, fmt.Sprintf("%s:%d: %q is not a table name", familiesFile, n+1, t))
			case listed[t] != "":
				problems = append(problems, fmt.Sprintf("%s:%d: table %s is already listed under feature %s", familiesFile, n+1, t, listed[t]))
			default:
				listed[t] = f[0]
				fam[f[0]] = append(fam[f[0]], t)
			}
		}
	}
	return fam, problems, nil
}

var identRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
