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

// wsWith is a workspaces migration whose Up also runs stmt.
func wsWith(stmt string) string {
	return "-- feature: workspaces\n-- +goose Up\nCREATE TABLE workspaces (id int);\n" + stmt + "\n-- +goose Down\nDROP TABLE workspaces;\n"
}

func writeMigration(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, "internal/store/migrations", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
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
		{"generic feature name holding several tables", map[string]string{"0001_misc.sql": "-- feature: misc\n-- +goose Up\nCREATE TABLE orgs (id int);\nCREATE TABLE workspaces (id int);\nCREATE TABLE audit_log (id int);\n-- +goose Down\nDROP TABLE audit_log;\nDROP TABLE workspaces;\nDROP TABLE orgs;\n"}, "too generic"},
		{"feature creating a table of another family", map[string]string{"0001_orgs.sql": "-- feature: orgs\n-- +goose Up\nCREATE TABLE orgs (id int);\nCREATE TABLE audit_log (id int);\n-- +goose Down\nDROP TABLE audit_log;\nDROP TABLE orgs;\n"}, "table audit_log does not belong to feature orgs"},
		{"quoted ALTER of another feature's table", map[string]string{"0001_orgs.sql": goodMigration("orgs"), "0002_workspaces.sql": wsWith("ALTER TABLE \"orgs\" ADD COLUMN x int;")}, "touches table orgs owned by feature orgs"},
		{"schema-qualified quoted ALTER", map[string]string{"0001_orgs.sql": goodMigration("orgs"), "0002_workspaces.sql": wsWith("ALTER TABLE public.\"orgs\" ADD COLUMN x int;")}, "touches table orgs"},
		{"ALTER TABLE IF EXISTS", map[string]string{"0001_orgs.sql": goodMigration("orgs"), "0002_workspaces.sql": wsWith("ALTER TABLE IF EXISTS orgs ADD COLUMN x int;")}, "touches table orgs"},
		{"trigger on another feature's table", map[string]string{"0001_orgs.sql": goodMigration("orgs"), "0002_workspaces.sql": wsWith("CREATE TRIGGER t BEFORE INSERT ON orgs FOR EACH ROW EXECUTE FUNCTION f();")}, "touches table orgs"},
		{"policy on another feature's table", map[string]string{"0001_orgs.sql": goodMigration("orgs"), "0002_workspaces.sql": wsWith("CREATE POLICY p ON orgs USING (true);")}, "touches table orgs"},
		{"comment on another feature's column", map[string]string{"0001_orgs.sql": goodMigration("orgs"), "0002_workspaces.sql": wsWith("COMMENT ON COLUMN orgs.name IS 'x';")}, "touches table orgs"},
		{"grant on another feature's table", map[string]string{"0001_orgs.sql": goodMigration("orgs"), "0002_workspaces.sql": wsWith("GRANT SELECT ON TABLE orgs TO reader;")}, "touches table orgs"},
		{"merge into another feature's table", map[string]string{"0001_orgs.sql": goodMigration("orgs"), "0002_workspaces.sql": wsWith("MERGE INTO orgs USING workspaces ON true WHEN MATCHED THEN DO NOTHING;")}, "touches table orgs"},
		{"subdirectory in the migrations dir", map[string]string{"0001_orgs.sql": goodMigration("orgs"), "old/0002_x.sql": goodMigration("x")}, "not a migration"},
		{"upper-case extension", map[string]string{"0001_orgs.SQL": goodMigration("orgs")}, "not a migration"},
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

func TestFamilyManifestAllowsAnOwnedTable(t *testing.T) {
	root := t.TempDir()
	body := "-- feature: ledger\n-- +goose Up\nCREATE TABLE calls (id int);\n-- +goose Down\nDROP TABLE calls;\n"
	writeMigration(t, root, "0001_ledger.sql", body)
	if code, _, errs := gen(t, root, "migrations"); code != 1 || !strings.Contains(errs, "table calls does not belong") {
		t.Fatalf("without a manifest entry: %d %q", code, errs)
	}
	if err := os.WriteFile(filepath.Join(root, "internal/store/migration-families.txt"), []byte("# owners\nledger calls # the ledger\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := gen(t, root, "migrations"); code != 0 {
		t.Fatalf("with a manifest entry: %q", errs)
	}
}

func TestFamilyManifestCannotLaunderMigrations(t *testing.T) {
	four := "-- feature: workspaces\n-- +goose Up\nCREATE TABLE workspaces (id int);\nCREATE TABLE audit_log (id int);\nCREATE TABLE calls (id int);\nCREATE TABLE orgs (id int);\n-- +goose Down\nDROP TABLE orgs;\nDROP TABLE calls;\nDROP TABLE audit_log;\nDROP TABLE workspaces;\n"
	ledger := "-- feature: ledger\n-- +goose Up\nCREATE TABLE calls (id int);\n-- +goose Down\nDROP TABLE calls;\n"
	tests := []struct {
		name, migration, file, manifest, want string
	}{
		{"the one-line launder of a four-table migration", four, "0001_workspaces.sql", "workspaces audit_log calls orgs\n", "ledger table audit_log may only belong to feature ledger or audit"},
		{"ledger table for a non-owner", four, "0001_workspaces.sql", "workspaces calls\n", "ledger table calls may only belong"},
		{"stale entry", ledger, "0001_ledger.sql", "ledger calls ghosts\n", "no migration creates it (stale entry)"},
		{"table owned by another feature", ledger, "0001_ledger.sql", "audit calls\n", "but feature ledger created it"},
		{"table listed twice", ledger, "0001_ledger.sql", "ledger calls\nledger calls\n", "already listed under feature ledger"},
		{"garbage line", ledger, "0001_ledger.sql", "ledger\n", "want `<feature> <table>"},
		{"bad table name", ledger, "0001_ledger.sql", "ledger Calls!\n", "is not a table name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeMigration(t, root, tc.file, tc.migration)
			if err := os.WriteFile(filepath.Join(root, "internal/store/migration-families.txt"), []byte(tc.manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			if code, _, errs := gen(t, root, "migrations"); code != 1 || !strings.Contains(errs, tc.want) {
				t.Fatalf("want exit 1 mentioning %q, got %d %q", tc.want, code, errs)
			}
		})
	}
}

func TestFeaturePrefixIsAFamily(t *testing.T) {
	root := t.TempDir()
	writeMigration(t, root, "0001_calls.sql", "-- feature: calls\n-- +goose Up\nCREATE TABLE calls (id int);\nCREATE TABLE calls_2026 (id int);\n-- +goose Down\nDROP TABLE calls_2026;\nDROP TABLE calls;\n")
	writeMigration(t, root, "0002_workspaces.sql", "-- feature: workspaces\n-- +goose Up\nCREATE TABLE workspaces (id int);\nCREATE TABLE workspaces_v (id int);\n-- +goose Down\nDROP TABLE workspaces_v;\nDROP TABLE workspaces;\n")
	if code, _, errs := gen(t, root, "migrations"); code != 0 {
		t.Fatalf("calls_2026 and workspaces_v must pass: %s", errs)
	}
}

func TestAliasedUpdateAndBlockCommentsAreSeen(t *testing.T) {
	root := t.TempDir()
	writeMigration(t, root, "0001_orgs.sql", goodMigration("orgs"))
	for _, stmt := range []string{"UPDATE orgs o SET name = 'x';", "UPDATE ONLY orgs AS o SET name = 'x';", "ALTER/**/TABLE orgs ADD COLUMN y int;", "COPY orgs FROM STDIN;"} {
		writeMigration(t, root, "0002_workspaces.sql", wsWith(stmt))
		if code, _, errs := gen(t, root, "migrations"); code != 1 || !strings.Contains(errs, "touches table orgs") {
			t.Errorf("%s not flagged: %d %q", stmt, code, errs)
		}
	}
}

func TestStemAndViews(t *testing.T) {
	for in, want := range map[string]string{"workspaces": "workspace", "policies": "policy", "audit": "audit"} {
		if got := stem(in); got != want {
			t.Errorf("stem(%q) = %q, want %q", in, got, want)
		}
	}
	m := parseMigration("0001_orgs.sql", "-- feature: orgs\n-- +goose Up\nCREATE VIEW org_names AS SELECT 1;\nCREATE TABLE IF NOT EXISTS public.\"orgs\" (id int) ;\n-- +goose Down\nDROP VIEW org_names;\n")
	if len(m.Created) != 2 {
		t.Fatalf("created %v", m.Created)
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
