package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

func TestMapAndEmbeddedTypesReachTheTwin(t *testing.T) {
	if got, err := tsType(reflect.TypeFor[map[contract.TrimStage]bool]()); err != nil || got != "Record<string, boolean>" {
		t.Fatalf("tsType(map) = %q, %v", got, err)
	}
	if _, err := tsType(reflect.TypeFor[map[int]bool]()); err == nil {
		t.Fatal("a map with a non-string key must be a generator error")
	}
	s, err := schemaFor(reflect.TypeFor[map[contract.TrimStage]bool]())
	if err != nil || s["type"] != "object" {
		t.Fatalf("schemaFor(map) = %v, %v", s, err)
	}
	ts, err := tsInterface(contract.DTO{Name: "CauseCard", Type: reflect.TypeFor[contract.CauseCard]()})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"  cause: SavingsCause;", "  kind: CauseKind;", "  topClient: string | null;", "  savedUsd: string;"} {
		if !strings.Contains(ts, want) {
			t.Errorf("CauseCard twin lacks %q in\n%s", want, ts)
		}
	}
	if strings.Contains(ts, "CauseView") {
		t.Errorf("an embedded struct must be flattened, got\n%s", ts)
	}
}

// The saver enums have one source, Go; the twin gets them as const tuples.
func TestContractTSEmitsTheSaverEnums(t *testing.T) {
	b, err := genContractTS()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`export const SAVINGS_CAUSES = ["duplicate_tool_call", "repeated_read", "no_progress", "step_budget", "reasoning_loop", "second_guessing", "trim", "router", "memory"] as const;`,
		`export const CAUSE_KINDS = ["monitor", "trim", "router", "memory"] as const;`,
		`export const STEP_EVENT_KINDS = ["monitor_fire", "trim", "route", "memory_hint"] as const;`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("generated.ts lacks %q", want)
		}
	}
}

func TestA204RouteHasNoResponseBodyInOpenAPI(t *testing.T) {
	op := operation(contract.Route{Method: "DELETE", Pattern: "/x", Status: 204, Response: "ForgetResult"})
	success := op["responses"].(map[string]any)["204"].(map[string]any)
	if _, ok := success["content"]; ok {
		t.Fatalf("a 204 must not declare a body: %v", success)
	}
}

func TestNullableTagMarksASliceThatCanBeNull(t *testing.T) {
	type cfg struct {
		Map  []string `json:"map" ts:"nullable"`
		List []string `json:"list"`
	}
	ts, err := tsInterface(contract.DTO{Name: "Cfg", Type: reflect.TypeFor[cfg]()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ts, "map: string[] | null;") || !strings.Contains(ts, "list: string[];") {
		t.Errorf("twin:\n%s", ts)
	}
	oa, err := objectSchema(contract.DTO{Name: "Cfg", Type: reflect.TypeFor[cfg]()})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := oa["properties"].(map[string]any)["map"].(map[string]any)["oneOf"]; !ok {
		t.Errorf("openapi: %v", oa)
	}
}

func TestMultiColumnAlterYieldsEveryColumnUntruncated(t *testing.T) {
	sql := "-- feature: calls\n-- +goose Up\nALTER TABLE calls\n" +
		"  ADD COLUMN trim_action text CHECK (trim_action IN ('acted','observe')),\n" +
		"  ADD COLUMN IF NOT EXISTS trim_tokens bigint NOT NULL DEFAULT 0,\n" +
		"  ADD COLUMN hint_injected boolean NOT NULL DEFAULT false;\n" +
		"-- +goose Down\nALTER TABLE calls DROP COLUMN hint_injected;\n"
	m := parseMigration("0018_calls.sql", sql)
	got := m.adds["calls"]
	if len(got) != 3 {
		t.Fatalf("got %d columns, want 3: %+v", len(got), got)
	}
	want := map[string]string{
		"trim_action":   "text CHECK (trim_action IN ('acted','observe'))",
		"trim_tokens":   "bigint NOT NULL DEFAULT 0",
		"hint_injected": "boolean NOT NULL DEFAULT false",
	}
	for _, c := range got {
		if want[c.Name] != c.Def {
			t.Errorf("%s: def %q, want %q", c.Name, c.Def, want[c.Name])
		}
	}
}
