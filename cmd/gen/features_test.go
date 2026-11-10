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
