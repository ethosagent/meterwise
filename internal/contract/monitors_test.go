package contract_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

func TestMonitorNamesFixedOrder(t *testing.T) {
	want := []contract.MonitorName{"duplicate_tool_call", "repeated_read", "no_progress",
		"step_budget", "reasoning_loop", "second_guessing"}
	got := contract.MonitorNames()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	got[0] = "mutated"
	if contract.MonitorNames()[0] != "duplicate_tool_call" {
		t.Fatal("MonitorNames must return a fresh slice")
	}
}

func TestDefaultMonitorConfig(t *testing.T) {
	d := contract.DefaultMonitorConfig()
	want := contract.MonitorConfig{
		DuplicateToolCall: contract.DuplicateToolCallConfig{Mode: "observe", MaxRepeats: 2, LookBack: 5, FuzzyPct: 0, ABAB: false},
		RepeatedRead:      contract.RepeatedReadConfig{Mode: "observe", MinReads: 3, BlockAfter: 0},
		NoProgress:        contract.NoProgressConfig{Mode: "observe", SimilarityPct: 95},
		StepBudget:        contract.StepBudgetConfig{Mode: "observe", MaxSteps: 40},
		ReasoningLoop:     contract.ReasoningLoopConfig{Mode: "observe", ShingleTokens: 40},
		SecondGuessing:    contract.SecondGuessingConfig{Mode: "observe", MinPhrases: 2},
	}
	if d != want {
		t.Fatalf("defaults = %+v, want %+v", d, want)
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
}

func TestAttrRoundTrip(t *testing.T) {
	custom := contract.DefaultMonitorConfig()
	custom.DuplicateToolCall = contract.DuplicateToolCallConfig{Mode: contract.ModeBlock, MaxRepeats: 4, LookBack: 20, FuzzyPct: 10, ABAB: true}
	custom.RepeatedRead.Mode = contract.ModeNote
	custom.RepeatedRead.BlockAfter = 9
	custom.StepBudget.MaxSteps = 200
	for name, c := range map[string]contract.MonitorConfig{"default": contract.DefaultMonitorConfig(), "custom": custom} {
		t.Run(name, func(t *testing.T) {
			a := c.Attr()
			if got := contract.ParseMonitorAttr(a); got != c {
				t.Fatalf("round trip = %+v, want %+v", got, c)
			}
			if again := contract.ParseMonitorAttr(a).Attr(); again != a {
				t.Fatalf("Attr not canonical: %s vs %s", again, a)
			}
			if !json.Valid([]byte(a)) || strings.ContainsAny(a, " \n") {
				t.Fatalf("Attr not compact JSON: %q", a)
			}
		})
	}
}

func TestMonitorConfigFromTolerant(t *testing.T) {
	d := contract.DefaultMonitorConfig()
	partial := d
	partial.StepBudget.MaxSteps = 7
	partial.NoProgress.Mode = contract.ModeBlock
	tests := []struct {
		name string
		in   any
		want contract.MonitorConfig
	}{
		{"nil", nil, d},
		{"empty string", "", d},
		{"empty object", map[string]any{}, d},
		{"syntax error", "{not json", d},
		{"not an object", []any{1, 2}, d},
		{"number", 42, d},
		{"unmarshalable", make(chan int), d},
		{"typed nil pointer", (*contract.MonitorConfig)(nil), d},
		{"json null", "null", d},
		{"partial map", map[string]any{
			"step_budget": map[string]any{"maxSteps": 7},
			"no_progress": map[string]any{"mode": "block"},
		}, partial},
		{"partial raw json", json.RawMessage(`{"step_budget":{"maxSteps":7},"no_progress":{"mode":"block"}}`), partial},
		{"partial bytes", []byte(`{"step_budget":{"maxSteps":7},"no_progress":{"mode":"block"}}`), partial},
		{"wrong type keeps rest", `{"step_budget":{"maxSteps":"lots"},"no_progress":{"mode":"block"}}`,
			func() contract.MonitorConfig { c := d; c.NoProgress.Mode = contract.ModeBlock; return c }()},
		{"float threshold is skipped", map[string]any{"step_budget": map[string]any{"maxSteps": 7.5}}, d},
		{"unknown mode defaults", map[string]any{"repeated_read": map[string]any{"mode": "explode", "minReads": 4}},
			func() contract.MonitorConfig { c := d; c.RepeatedRead.MinReads = 4; return c }()},
		{"out of range defaults", map[string]any{"step_budget": map[string]any{"maxSteps": -5}, "no_progress": map[string]any{"similarityPct": 500}}, d},
		{"unknown keys ignored", map[string]any{"bogus": 1, "step_budget": map[string]any{"extra": true}}, d},
		{"typed config sanitized", contract.MonitorConfig{}, d},
		{"typed pointer", &partial, partial},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contract.MonitorConfigFrom(tt.in); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseMonitorAttrBad(t *testing.T) {
	d := contract.DefaultMonitorConfig()
	for _, s := range []string{"", "   ", "garbage", "{", `{"step_budget":`, "[]", `"x"`} {
		if got := contract.ParseMonitorAttr(s); got != d {
			t.Errorf("ParseMonitorAttr(%q) = %+v, want defaults", s, got)
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		mut    func(*contract.MonitorConfig)
		detail string // empty means valid
	}{
		{"defaults", func(*contract.MonitorConfig) {}, ""},
		{"all block", func(c *contract.MonitorConfig) { c.NoProgress.Mode = contract.ModeBlock }, ""},
		{"bounds ok", func(c *contract.MonitorConfig) {
			c.DuplicateToolCall.MaxRepeats = 100
			c.DuplicateToolCall.LookBack = 50
			c.NoProgress.SimilarityPct = 50
			c.SecondGuessing.MinPhrases = 20
		}, ""},
		{"empty mode", func(c *contract.MonitorConfig) { c.DuplicateToolCall.Mode = "" }, "duplicate_tool_call.mode: must be one of observe, note, block"},
		{"unknown mode", func(c *contract.MonitorConfig) { c.StepBudget.Mode = "kill" }, "step_budget.mode: must be one of observe, note, block"},
		{"maxRepeats low", func(c *contract.MonitorConfig) { c.DuplicateToolCall.MaxRepeats = 1 }, "duplicate_tool_call.maxRepeats: must be between 2 and 100"},
		{"lookBack high", func(c *contract.MonitorConfig) { c.DuplicateToolCall.LookBack = 51 }, "duplicate_tool_call.lookBack: must be between 1 and 50"},
		{"fuzzyPct high", func(c *contract.MonitorConfig) { c.DuplicateToolCall.FuzzyPct = 101 }, "duplicate_tool_call.fuzzyPct: must be between 0 and 100"},
		{"minReads low", func(c *contract.MonitorConfig) { c.RepeatedRead.MinReads = 1 }, "repeated_read.minReads: must be between 2 and 100"},
		{"blockAfter negative", func(c *contract.MonitorConfig) { c.RepeatedRead.BlockAfter = -1 }, "repeated_read.blockAfter: must be between 0 and 1000"},
		{"similarity low", func(c *contract.MonitorConfig) { c.NoProgress.SimilarityPct = 49 }, "no_progress.similarityPct: must be between 50 and 100"},
		{"maxSteps zero", func(c *contract.MonitorConfig) { c.StepBudget.MaxSteps = 0 }, "step_budget.maxSteps: must be between 1 and 10000"},
		{"shingle low", func(c *contract.MonitorConfig) { c.ReasoningLoop.ShingleTokens = 2 }, "reasoning_loop.shingleTokens: must be between 3 and 500"},
		{"phrases zero", func(c *contract.MonitorConfig) { c.SecondGuessing.MinPhrases = 0 }, "second_guessing.minPhrases: must be between 1 and 20"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := contract.DefaultMonitorConfig()
			tt.mut(&c)
			err := c.Validate()
			if tt.detail == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var ce *contract.Error
			if !errors.As(err, &ce) || ce.Code != contract.CodeValidationFailed {
				t.Fatalf("err = %v, want validation_failed", err)
			}
			if !strings.Contains(strings.Join(ce.Details, "|"), tt.detail) {
				t.Fatalf("details = %q, want %q", ce.Details, tt.detail)
			}
		})
	}
}

func TestVerdictEvidenceJSON(t *testing.T) {
	b, err := json.Marshal(contract.MonitorVerdict{Monitor: contract.MonitorRepeatedRead, Mode: contract.ModeNote, Action: contract.ModeNote,
		Evidence: contract.MonitorEvidence{MatchedCalls: []int{1, 3}, Tool: "read", PathHash: "abcd", Count: 3, Threshold: 3}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"monitor":"repeated_read","mode":"note","action":"note","evidence":{"matchedCalls":[1,3],"tool":"read","pathHash":"abcd","count":3,"threshold":3},"basis":{}}`
	if string(b) != want {
		t.Fatalf("json = %s\nwant   %s", b, want)
	}
}

func verdict(m contract.MonitorName) contract.MonitorVerdict {
	return contract.MonitorVerdict{Monitor: m, Mode: contract.ModeObserve, Action: contract.ModeObserve}
}

func TestCallVerdictsAddSealNotes(t *testing.T) {
	cv := &contract.CallVerdicts{}
	if !cv.Add(verdict("a")) || !cv.Add(verdict("b")) {
		t.Fatal("first adds must succeed")
	}
	if cv.Add(verdict("a")) {
		t.Fatal("duplicate monitor must be rejected")
	}
	cv.AddNotes(-3)
	cv.AddNotes(0)
	cv.AddNotes(1)
	cv.AddNotes(5)
	list, notes := cv.Seal()
	if len(list) != 2 || list[0].Monitor != "a" || list[1].Monitor != "b" {
		t.Fatalf("list = %+v", list)
	}
	if notes != contract.MaxNotesPerCall {
		t.Fatalf("notes = %d, want capped at 2", notes)
	}
	if cv.Add(verdict("c")) {
		t.Fatal("Add after Seal must return false")
	}
	cv.AddNotes(1)
	list2, notes2 := cv.Seal()
	if len(list2) != 2 || notes2 != 2 {
		t.Fatalf("sealed state changed: %d verdicts, %d notes", len(list2), notes2)
	}
	list[0].Monitor = "mutated"
	if l3, _ := cv.Seal(); l3[0].Monitor != "a" {
		t.Fatal("Seal must return a copy")
	}
}

func TestCallVerdictsEmptySeal(t *testing.T) {
	list, notes := (&contract.CallVerdicts{}).Seal()
	if len(list) != 0 || notes != 0 {
		t.Fatalf("empty seal = %v, %d", list, notes)
	}
}

func TestCallVerdictsConcurrent(t *testing.T) {
	cv := &contract.CallVerdicts{}
	names := contract.MonitorNames()
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, n := range names {
				if cv.Add(verdict(n)) {
					accepted.Add(1)
				}
				cv.AddNotes(1)
			}
		}()
	}
	wg.Wait()
	list, notes := cv.Seal()
	if got := accepted.Load(); got != int64(len(names)) || len(list) != len(names) {
		t.Fatalf("accepted %d, list %d, want %d each (one per monitor)", got, len(list), len(names))
	}
	if notes != 2 {
		t.Fatalf("notes = %d, want 2", notes)
	}
}

func TestCallVerdictsSealRace(t *testing.T) {
	cv := &contract.CallVerdicts{}
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for _, n := range contract.MonitorNames() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cv.Add(verdict(n)) {
				accepted.Add(1)
			}
		}()
	}
	list, _ := cv.Seal()
	wg.Wait()
	after, _ := cv.Seal()
	if len(list) != len(after) || int64(len(after)) != accepted.Load() {
		t.Fatalf("an Add that returned true must be in the seal: sealed %d, final %d, accepted %d", len(list), len(after), accepted.Load())
	}
}
