package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/nonet"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

func gen(t *testing.T, root string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(append([]string{"-root", root}, args...), &out, &errb)
	return code, out.String(), errb.String()
}

func TestGenerateThenCheckIsClean(t *testing.T) {
	root := t.TempDir()
	if code, out, errs := gen(t, root); code != 0 || strings.Count(out, "wrote ") != 3 {
		t.Fatalf("generate: %d %q %q", code, out, errs)
	}
	if code, _, errs := gen(t, root, "-check"); code != 0 {
		t.Fatalf("check right after generate failed: %s", errs)
	}
	if code, out, _ := gen(t, root); code != 0 || strings.Contains(out, "wrote") {
		t.Fatalf("second generate must be a no-op, got %q", out)
	}
}

func TestCheckFailsOnHandEditStaleAndMissing(t *testing.T) {
	root := t.TempDir()
	gen(t, root)
	ts := filepath.Join(root, "web/src/contract/generated.ts")
	b, _ := os.ReadFile(ts)
	if err := os.WriteFile(ts, append(b, "export const sneaky = 1;\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := gen(t, root, "-check")
	if code != 1 || !strings.Contains(errs, "web/src/contract/generated.ts is stale or was edited by hand") {
		t.Fatalf("hand edit not caught: %d %q", code, errs)
	}
	if err := os.Remove(filepath.Join(root, "internal/api/openapi.json")); err != nil {
		t.Fatal(err)
	}
	if code, _, errs = gen(t, root, "-check", "openapi"); code != 1 || !strings.Contains(errs, "is missing") {
		t.Fatalf("missing file not caught: %d %q", code, errs)
	}
	if code, _, errs = gen(t, root, "-check", "schema"); code != 0 {
		t.Fatalf("untouched target must pass: %s", errs)
	}
}

func TestSchemaGoesStaleWhenAMigrationChanges(t *testing.T) {
	root := t.TempDir()
	gen(t, root)
	writeMigration(t, root, "0001_orgs.sql", goodMigration("orgs"))
	code, _, errs := gen(t, root, "-check", "schema")
	if code != 1 || !strings.Contains(errs, "docs/schema.md") {
		t.Fatalf("schema drift not caught: %d %q", code, errs)
	}
	gen(t, root, "schema")
	b, _ := os.ReadFile(filepath.Join(root, "docs/schema.md"))
	for _, want := range []string{"## orgs", "Owner: `orgs`", "| `id` | `uuid PRIMARY KEY` |", "| `name` | `text NOT NULL` |"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("schema.md lacks %q:\n%s", want, b)
		}
	}
}

func TestUnknownTargetAndBadFlag(t *testing.T) {
	if code, _, errs := gen(t, t.TempDir(), "bogus"); code != 2 || !strings.Contains(errs, "unknown target") {
		t.Fatalf("%d %q", code, errs)
	}
	if code, _, _ := gen(t, t.TempDir(), "-nope"); code != 2 {
		t.Fatalf("bad flag exit %d", code)
	}
}

func TestMigrationsLintSkipsLoudlyWhenEmpty(t *testing.T) {
	code, out, _ := gen(t, t.TempDir(), "migrations")
	if code != 0 || !strings.Contains(out, "SKIP migrations lint") {
		t.Fatalf("%d %q", code, out)
	}
}

func goodMigration(feature string) string {
	return "-- feature: " + feature + "\n-- +goose Up\nCREATE TABLE " + feature + " (\n  id uuid PRIMARY KEY,\n  name text NOT NULL,\n  UNIQUE (name)\n);\n-- +goose Down\nDROP TABLE " + feature + ";\n"
}

func writeMigration(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "internal/store/migrations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationLintGoesRedOnEachBadInput(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"touches another feature's table", map[string]string{
			"0001_orgs.sql":       goodMigration("orgs"),
			"0002_workspaces.sql": "-- feature: workspaces\n-- +goose Up\nCREATE TABLE workspaces (id uuid PRIMARY KEY, org_id uuid REFERENCES orgs(id));\nALTER TABLE orgs ADD COLUMN slug text;\n-- +goose Down\nDROP TABLE workspaces;\n",
		}, "touches table orgs owned by feature orgs"},
		{"missing feature header", map[string]string{"0001_orgs.sql": "-- +goose Up\nCREATE TABLE orgs (id int);\n-- +goose Down\nDROP TABLE orgs;\n"}, "first lines need `-- feature: orgs`"},
		{"header disagrees with the file name", map[string]string{"0001_orgs.sql": strings.Replace(goodMigration("orgs"), "feature: orgs", "feature: users", 1)}, "first lines need"},
		{"no down section", map[string]string{"0001_orgs.sql": "-- feature: orgs\n-- +goose Up\nCREATE TABLE orgs (id int);\n"}, "missing a non-empty `-- +goose Down`"},
		{"empty down section", map[string]string{"0001_orgs.sql": "-- feature: orgs\n-- +goose Up\nCREATE TABLE orgs (id int);\n-- +goose Down\n-- nothing\n"}, "missing a non-empty"},
		{"bad file name", map[string]string{"orgs.sql": goodMigration("orgs")}, "name must be NNNN_<feature>.sql"},
		{"duplicate number", map[string]string{"0001_orgs.sql": goodMigration("orgs"), "0001_users.sql": goodMigration("users")}, "number 0001 already used"},
		{"alters a table nobody created", map[string]string{"0001_orgs.sql": "-- feature: orgs\n-- +goose Up\nALTER TABLE ghosts ADD COLUMN x int;\n-- +goose Down\nALTER TABLE ghosts DROP COLUMN x;\n"}, "no earlier migration creates"},
		{"creates the same table twice", map[string]string{"0001_orgs.sql": goodMigration("orgs"), "0002_orgs.sql": goodMigration("orgs")}, "already created"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for n, b := range tc.files {
				writeMigration(t, root, n, b)
			}
			code, _, errs := gen(t, root, "migrations")
			if code != 1 || !strings.Contains(errs, tc.want) {
				t.Fatalf("want exit 1 mentioning %q, got %d %q", tc.want, code, errs)
			}
		})
	}
}

func TestMigrationLintAcceptsOneFeaturePerFile(t *testing.T) {
	root := t.TempDir()
	writeMigration(t, root, "0001_orgs.sql", goodMigration("orgs"))
	writeMigration(t, root, "0002_workspaces.sql", "-- feature: workspaces\n-- +goose Up\nCREATE TABLE workspaces (id uuid PRIMARY KEY, org_id uuid REFERENCES orgs(id));\nCREATE INDEX workspaces_org_idx ON workspaces (org_id);\n-- +goose Down\nDROP TABLE workspaces;\n")
	writeMigration(t, root, "0003_workspaces.sql", "-- feature: workspaces\n-- +goose Up\nALTER TABLE workspaces ADD COLUMN archived_at timestamptz;\n-- +goose Down\nALTER TABLE workspaces DROP COLUMN archived_at;\n")
	if code, _, errs := gen(t, root, "migrations"); code != 0 {
		t.Fatalf("a clean one-feature-per-file history was rejected: %s", errs)
	}
}

func TestSchemaListsAddedColumns(t *testing.T) {
	root := t.TempDir()
	writeMigration(t, root, "0001_orgs.sql", goodMigration("orgs"))
	writeMigration(t, root, "0002_orgs.sql", "-- feature: orgs\n-- +goose Up\nALTER TABLE orgs ADD COLUMN slug text NOT NULL DEFAULT '';\n-- +goose Down\nALTER TABLE orgs DROP COLUMN slug;\n")
	if code, _, errs := gen(t, root, "migrations"); code != 0 {
		t.Fatalf("lint: %s", errs)
	}
	gen(t, root, "schema")
	b, _ := os.ReadFile(filepath.Join(root, "docs/schema.md"))
	if !strings.Contains(string(b), "| `slug` | `text NOT NULL DEFAULT ''` | 0002_orgs.sql |") {
		t.Fatalf("added column missing:\n%s", b)
	}
}

type unsupported struct{ C chan int }

func TestUnsupportedFieldTypesFailLoudly(t *testing.T) {
	if _, err := tsType(reflect.TypeFor[chan int]()); err == nil {
		t.Fatal("tsType accepted a chan")
	}
	if _, err := schemaFor(reflect.TypeFor[chan int]()); err == nil {
		t.Fatal("schemaFor accepted a chan")
	}
	d := contract.DTO{Name: "Bad", Type: reflect.TypeFor[unsupported]()}
	if _, err := tsInterface(d); err == nil {
		t.Fatal("tsInterface accepted a chan field")
	}
	if _, err := objectSchema(d); err == nil {
		t.Fatal("objectSchema accepted a chan field")
	}
}

func TestTypeMappings(t *testing.T) {
	tests := map[reflect.Type]string{
		reflect.TypeFor[string]():            "string",
		reflect.TypeFor[bool]():              "boolean",
		reflect.TypeFor[int64]():             "number",
		reflect.TypeFor[[]string]():          "string[]",
		reflect.TypeFor[contract.MicroUSD](): "string",
		reflect.TypeFor[contract.Code]():     "ErrorCode",
		reflect.TypeFor[contract.Health]():   "Health",
	}
	for ty, want := range tests {
		if got, err := tsType(ty); err != nil || got != want {
			t.Errorf("tsType(%s) = %q, %v; want %q", ty, got, err, want)
		}
		if _, err := schemaFor(ty); err != nil {
			t.Errorf("schemaFor(%s): %v", ty, err)
		}
	}
	if got := operationID(contract.Route{Method: "GET", Pattern: "/api/v1/workspaces/{id}"}); got != "getApiV1WorkspacesId" {
		t.Errorf("operationID = %q", got)
	}
}

func TestFirstDiffLine(t *testing.T) {
	if got := firstDiffLine([]byte("a\nb\nc"), []byte("a\nX\nc")); got != 2 {
		t.Errorf("got %d", got)
	}
	if got := firstDiffLine([]byte("a"), []byte("a\nb")); got != 2 {
		t.Errorf("got %d", got)
	}
}
