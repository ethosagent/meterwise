package monitors

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

const fixtureWS = "ws-fixture"

// ringEntry is a CallSummary that can also derive its shingles from synthetic text.
type ringEntry struct {
	contract.CallSummary
	ShinglesFrom string
	// ArgsFrom are the arguments of the entry's first tool: its ArgsHashes become their real hash, so a
	// stream fixture can name the same call.
	ArgsFrom string
}

type fixture struct {
	Description  string
	Ring         []ringEntry
	SessionCalls int             `json:"session_calls"`
	Config       json.RawMessage `json:"config"`
	Stream       *struct {
		Text      string
		Reasoning string
		Tools     []string
		Args      []string // arguments per tool, in order; missing means none
	}
	Expect struct {
		Fires    bool
		Evidence *contract.MonitorEvidence
	}
}

type ringView []contract.CallSummary

func (r ringView) Calls() []contract.CallSummary { return r }

func loadFixture(t *testing.T, path string) (fixture, *tollgate.Call, ringView, contract.MonitorConfig) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	cfg := contract.DefaultMonitorConfig()
	if len(f.Config) > 0 {
		if err := json.Unmarshal(f.Config, &cfg); err != nil {
			t.Fatalf("%s config: %v", path, err)
		}
	}
	var ring ringView
	for _, e := range f.Ring {
		if e.ShinglesFrom != "" {
			e.Shingles = contract.TurnShingles("", e.ShinglesFrom)
		}
		if e.ArgsFrom != "" {
			e.ArgsHashes = []uint64{contract.ToolArgsHash(e.Tools[0], []byte(e.ArgsFrom))}
		}
		ring = append(ring, e.CallSummary)
	}
	c := &tollgate.Call{Tenant: tollgate.Tenant{ID: fixtureWS}, Session: tollgate.Session{Calls: f.SessionCalls}, Meta: map[string]any{}}
	if f.Stream != nil {
		st := &callState{cfg: cfg}
		st.event(tollgate.Event{Kind: tollgate.EventTextDelta, Text: f.Stream.Text})
		st.event(tollgate.Event{Kind: tollgate.EventReasoningDelta, Text: f.Stream.Reasoning})
		for i, name := range f.Stream.Tools {
			var args string
			if i < len(f.Stream.Args) {
				args = f.Stream.Args[i]
			}
			st.event(tollgate.Event{Kind: tollgate.EventToolCallDelta, ToolCall: &tollgate.ToolCallDelta{Index: i, Name: name, Arguments: args}})
		}
		c.Meta[metaState] = st
	}
	return f, c, ring, cfg
}

// resolve expands the fixture's placeholders: "@N" is the workspace-salted id of ring hash N, and
// "@shingle:TEXT" the id of the first shingle of TEXT.
func resolve(ev contract.MonitorEvidence) contract.MonitorEvidence {
	one := func(s string) string {
		switch {
		case strings.HasPrefix(s, "@shingle:"):
			sh := contract.ShinglesOf(contract.Tokens(strings.TrimPrefix(s, "@shingle:"), 0), 1)
			return contract.HashID(fixtureWS, contract.HashValue(sh[0]))
		case strings.HasPrefix(s, "@"):
			return contract.HashID(fixtureWS, strings.TrimPrefix(s, "@"))
		}
		return s
	}
	ev.ArgsHash, ev.PathHash, ev.Hash = one(ev.ArgsHash), one(ev.PathHash), one(ev.Hash)
	return ev
}

func TestDetectorsOnRingFixtures(t *testing.T) {
	total := 0
	defer func() {
		if total < 32 {
			t.Errorf("only %d fixtures ran", total)
		}
	}()
	for _, d := range Registry() {
		files, _ := filepath.Glob(filepath.Join("testdata", string(d.Name), "*.json"))
		total += len(files)
		var pos, neg int
		for _, p := range files {
			base := filepath.Base(p)
			pos += btoi(strings.HasPrefix(base, "positive"))
			neg += btoi(strings.HasPrefix(base, "negative"))
			t.Run(string(d.Name)+"/"+strings.TrimSuffix(base, ".json"), func(t *testing.T) {
				f, c, ring, cfg := loadFixture(t, p)
				v, ok := d.Detect(c, ring, cfg)
				if ok != f.Expect.Fires {
					t.Fatalf("fires = %v, want %v (%s)\nverdict %+v", ok, f.Expect.Fires, f.Description, v)
				}
				if !ok {
					return
				}
				want := resolve(*f.Expect.Evidence)
				if !reflect.DeepEqual(v.Evidence, want) || v.Monitor != d.Name {
					t.Errorf("evidence\n got  %+v\n want %+v", v.Evidence, want)
				}
				if v.Mode != contract.ModeObserve {
					t.Errorf("Mode = %q, want the configured default observe", v.Mode)
				}
			})
		}
		if pos == 0 || neg == 0 {
			t.Errorf("%s needs a positive and a negative fixture (have %d, %d)", d.Name, pos, neg)
		}
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestRegistryIsTheSixMonitorsInOrder(t *testing.T) {
	var got []contract.MonitorName
	for _, d := range Registry() {
		got = append(got, d.Name)
	}
	if !reflect.DeepEqual(got, contract.MonitorNames()) {
		t.Errorf("registry %v, want %v", got, contract.MonitorNames())
	}
	for _, d := range Registry() {
		if d.Stream != (d.Name == contract.MonitorReasoningLoop || d.Name == contract.MonitorSecondGuessing) {
			t.Errorf("%s Stream = %v", d.Name, d.Stream)
		}
	}
}

func TestDetectorsReportTheConfiguredMode(t *testing.T) {
	_, c, ring, cfg := loadFixture(t, "testdata/duplicate_tool_call/positive.json")
	cfg.DuplicateToolCall.Mode = contract.ModeBlock
	if v, ok := DetectDuplicateToolCall(c, ring, cfg); !ok || v.Mode != contract.ModeBlock {
		t.Errorf("%+v %v", v, ok)
	}
}

func TestFuzzyDuplicateComparesArgumentsOfTheRequestItself(t *testing.T) {
	args := func(s string) json.RawMessage { return json.RawMessage(s) }
	asst := func(id, a string) tollgate.Message {
		return tollgate.Message{Role: tollgate.RoleAssistant, ToolCalls: []tollgate.ToolCall{{ID: id, Name: "search", Arguments: args(a)}}}
	}
	c := &tollgate.Call{Tenant: tollgate.Tenant{ID: fixtureWS}, Messages: []tollgate.Message{
		{Role: tollgate.RoleUser, Text: "go"},
		asst("a", `{"query":"find the failing test in module alpha now"}`), {Role: tollgate.RoleTool, ToolCallID: "a", Text: "r"},
		asst("b", `{"query":"find the failing test in module alpha"}`), {Role: tollgate.RoleTool, ToolCallID: "b", Text: "r"},
	}}
	ring := ringView{
		{Index: 1, Tools: []string{"search"}, ArgsHashes: []uint64{1}},
		{Index: 2, Tools: []string{"search"}, ArgsHashes: []uint64{2}},
	}
	cfg := contract.DefaultMonitorConfig()
	if _, ok := DetectDuplicateToolCall(c, ring, cfg); ok {
		t.Fatal("fuzzy is off by default: different hashes must not fire")
	}
	cfg.DuplicateToolCall.FuzzyPct = 80
	v, ok := DetectDuplicateToolCall(c, ring, cfg)
	if !ok || !reflect.DeepEqual(v.Evidence.MatchedCalls, []int{1, 2}) || v.Evidence.Tool != "search" || v.Evidence.Count != 2 {
		t.Fatalf("fuzzy 80: %+v %v", v, ok)
	}
	cfg.DuplicateToolCall.FuzzyPct = 100
	if _, ok := DetectDuplicateToolCall(c, ring, cfg); ok {
		t.Error("fuzzy 100 needs identical token sets")
	}
}

func TestStreamDetectorsWithoutStateStayQuiet(t *testing.T) {
	c := &tollgate.Call{Tenant: tollgate.Tenant{ID: fixtureWS}, Meta: map[string]any{}}
	cfg := contract.DefaultMonitorConfig()
	if _, ok := DetectReasoningLoop(c, ringView{}, cfg); ok {
		t.Error("reasoning_loop without collected text")
	}
	if _, ok := DetectSecondGuessing(c, ringView{}, cfg); ok {
		t.Error("second_guessing without collected text")
	}
}

func TestDetectorsOnAnEmptyRingAreQuiet(t *testing.T) {
	c := &tollgate.Call{Tenant: tollgate.Tenant{ID: fixtureWS}, Session: tollgate.Session{Calls: 1}}
	cfg := contract.DefaultMonitorConfig()
	for _, d := range Registry() {
		if _, ok := d.Detect(c, ringView{}, cfg); ok {
			t.Errorf("%s fired on nothing", d.Name)
		}
	}
}
