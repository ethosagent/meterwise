package contract_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

// invalidAt asserts err is a validation error whose detail starts with the field path.
func invalidAt(t *testing.T, err error, path string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want a 422 at %q, got nil", path)
	}
	e := contract.AsError(err)
	if e.Code != contract.CodeValidationFailed || len(e.Details) == 0 || !strings.HasPrefix(e.Details[0], path+":") {
		t.Fatalf("want validation_failed at %q, got %v %v", path, e.Code, e.Details)
	}
}

func TestSaverDefaults(t *testing.T) {
	tc := contract.DefaultTrimConfig()
	if tc.Mode != contract.TrimModeObserve || tc.MaxFoldPct != 90 || tc.MinGainPct != 2 || len(tc.NeverTrimTools) != 0 || tc.NeverTrimTools == nil {
		t.Fatalf("trim default = %+v", tc)
	}
	for _, s := range contract.TrimStages() {
		if want := s != contract.StageSchemaTrim; tc.Stages[s] != want {
			t.Errorf("stage %s default = %v, want %v", s, tc.Stages[s], want)
		}
	}
	rc := contract.DefaultRouterConfig()
	if rc.Mode != contract.RouterModeOff || rc.ThresholdBp != nil || rc.Map != nil {
		t.Fatalf("router default = %+v", rc)
	}
	if rc.EffectiveThresholdBp() != 2500 || (contract.RouterConfig{Mode: contract.RouterModeAggressive}).EffectiveThresholdBp() != 5000 {
		t.Fatal("default thresholds are 2500 (conservative) and 5000 (aggressive)")
	}
	mc := contract.DefaultMemoryConfig()
	if mc != (contract.MemoryConfig{Mode: contract.MemoryModeOff, ThresholdBp: 8200, MaxHintsPerSession: 1}) {
		t.Fatalf("memory default = %+v", mc)
	}
	for name, err := range map[string]error{"trim": tc.Validate(), "router": rc.Validate(), "memory": mc.Validate()} {
		if err != nil {
			t.Errorf("%s default must validate: %v", name, err)
		}
	}
}

func TestSaverConfigsRoundTripThroughAttrAndFrom(t *testing.T) {
	thr := 4000
	trim := contract.TrimConfig{Mode: contract.TrimModeAggressive, NeverTrimTools: []string{"bash", "read"}, MaxFoldPct: 60, MinGainPct: 5,
		Stages: map[contract.TrimStage]bool{"repeat_collapse": false, "log_window": true, "dup_lines": true, "json_table": false, "schema_trim": true}}
	router := contract.RouterConfig{Mode: contract.RouterModeConservative, ThresholdBp: &thr, Map: []contract.RoutePair{{Provider: "openai", From: "gpt-5", To: "gpt-5-mini"}}}
	mem := contract.MemoryConfig{Mode: contract.MemoryModeHint, ThresholdBp: 9000, MaxHintsPerSession: 3, Model: "m", EmbedModel: "e"}
	if got := contract.ParseTrimAttr(trim.Attr()); !reflect.DeepEqual(got, trim) {
		t.Errorf("trim round trip = %+v", got)
	}
	if got := contract.ParseRouterAttr(router.Attr()); !reflect.DeepEqual(got, router) {
		t.Errorf("router round trip = %+v", got)
	}
	if got := contract.ParseMemoryAttr(mem.Attr()); got != mem {
		t.Errorf("memory round trip = %+v", got)
	}
	// the decoded-map form a settings column yields
	var m map[string]any
	if err := json.Unmarshal([]byte(trim.Attr()), &m); err != nil {
		t.Fatal(err)
	}
	if got := contract.TrimConfigFrom(m); !reflect.DeepEqual(got, trim) {
		t.Errorf("trim from map = %+v", got)
	}
	if got := contract.TrimConfigFrom(&trim); !reflect.DeepEqual(got, trim) {
		t.Errorf("trim from pointer = %+v", got)
	}
	// canonical: the same config always gives the same bytes
	if trim.Attr() != contract.ParseTrimAttr(trim.Attr()).Attr() {
		t.Error("Attr is not canonical")
	}
}

func TestSaverFromResetsBadFieldsOneByOne(t *testing.T) {
	got := contract.TrimConfigFrom(`{"mode":"bogus","maxFoldPct":5,"minGainPct":7,"neverTrimTools":["ok",""],"stages":{"dup_lines":false,"nope":true}}`)
	if got.Mode != contract.TrimModeObserve || got.MaxFoldPct != 90 || got.MinGainPct != 7 || len(got.NeverTrimTools) != 1 || got.Stages["dup_lines"] || len(got.Stages) != 5 {
		t.Errorf("trim from bad = %+v", got)
	}
	if got := contract.TrimConfigFrom(`{"mode":`); !reflect.DeepEqual(got, contract.DefaultTrimConfig()) {
		t.Errorf("a syntax error yields the defaults, got %+v", got)
	}
	if got := contract.TrimConfigFrom(nil); !reflect.DeepEqual(got, contract.DefaultTrimConfig()) {
		t.Errorf("nil yields the defaults, got %+v", got)
	}
	if got := contract.TrimConfigFrom(`{"mode":"lossless","maxFoldPct":"x"}`); got.Mode != contract.TrimModeLossless || got.MaxFoldPct != 90 {
		t.Errorf("a wrong-typed field is skipped and the rest kept: %+v", got)
	}
	r := contract.RouterConfigFrom(`{"mode":"x","thresholdBp":20000,"map":[{"provider":"openai","from":"a","to":"a"},{"provider":"openai","from":"a","to":"b"},{"provider":"openai","from":"a","to":"c"}]}`)
	if r.Mode != contract.RouterModeOff || r.ThresholdBp != nil || len(r.Map) != 1 || r.Map[0].To != "b" {
		t.Errorf("router from bad = %+v", r)
	}
	if got := contract.RouterConfigFrom(`{"map":[]}`); got.Map == nil || len(got.Map) != 0 {
		t.Errorf("an explicit empty map stays empty (no routes), got %#v", got.Map)
	}
	m := contract.MemoryConfigFrom(`{"mode":"hint","thresholdBp":10,"maxHintsPerSession":9}`)
	if m.Mode != contract.MemoryModeHint || m.ThresholdBp != 8200 || m.MaxHintsPerSession != 1 {
		t.Errorf("memory from bad = %+v", m)
	}
}

func TestSaverValidateNamesTheFieldPath(t *testing.T) {
	trim := func(f func(*contract.TrimConfig)) error {
		c := contract.DefaultTrimConfig()
		f(&c)
		return c.Validate()
	}
	invalidAt(t, trim(func(c *contract.TrimConfig) { c.Mode = "x" }), "mode")
	invalidAt(t, trim(func(c *contract.TrimConfig) { c.MaxFoldPct = 9 }), "maxFoldPct")
	invalidAt(t, trim(func(c *contract.TrimConfig) { c.MaxFoldPct = 101 }), "maxFoldPct")
	invalidAt(t, trim(func(c *contract.TrimConfig) { c.MinGainPct = 51 }), "minGainPct")
	invalidAt(t, trim(func(c *contract.TrimConfig) { c.MinGainPct = -1 }), "minGainPct")
	invalidAt(t, trim(func(c *contract.TrimConfig) { c.NeverTrimTools = make([]string, 51) }), "neverTrimTools")
	invalidAt(t, trim(func(c *contract.TrimConfig) { c.NeverTrimTools = []string{"a", ""} }), "neverTrimTools[1]")
	invalidAt(t, trim(func(c *contract.TrimConfig) { c.NeverTrimTools = []string{strings.Repeat("x", 129)} }), "neverTrimTools[0]")
	invalidAt(t, trim(func(c *contract.TrimConfig) { c.Stages["bogus"] = true }), "stages.bogus")
	if err := trim(func(c *contract.TrimConfig) {
		c.NeverTrimTools = []string{strings.Repeat("x", 128)}
		c.MaxFoldPct = 10
		c.MinGainPct = 50
	}); err != nil {
		t.Errorf("boundaries are valid: %v", err)
	}
	router := func(f func(*contract.RouterConfig)) error {
		c := contract.DefaultRouterConfig()
		f(&c)
		return c.Validate()
	}
	bad, zero := 10001, 0
	invalidAt(t, router(func(c *contract.RouterConfig) { c.Mode = "x" }), "mode")
	invalidAt(t, router(func(c *contract.RouterConfig) { c.ThresholdBp = &bad }), "thresholdBp")
	invalidAt(t, router(func(c *contract.RouterConfig) { c.ThresholdBp = &zero }), "thresholdBp")
	invalidAt(t, router(func(c *contract.RouterConfig) { c.Map = []contract.RoutePair{{Provider: "", From: "a", To: "b"}} }), "map[0].provider")
	invalidAt(t, router(func(c *contract.RouterConfig) { c.Map = []contract.RoutePair{{Provider: "p", From: "", To: "b"}} }), "map[0].from")
	invalidAt(t, router(func(c *contract.RouterConfig) { c.Map = []contract.RoutePair{{Provider: "p", From: "a", To: ""}} }), "map[0].to")
	invalidAt(t, router(func(c *contract.RouterConfig) { c.Map = []contract.RoutePair{{Provider: "p", From: "a", To: "a"}} }), "map[0].to")
	invalidAt(t, router(func(c *contract.RouterConfig) {
		c.Map = []contract.RoutePair{{Provider: "p", From: "a", To: "b"}, {Provider: "p", From: "a", To: "c"}}
	}), "map[1].from")
	invalidAt(t, router(func(c *contract.RouterConfig) { c.Map = make([]contract.RoutePair, 101) }), "map")
	mem := func(f func(*contract.MemoryConfig)) error {
		c := contract.DefaultMemoryConfig()
		f(&c)
		return c.Validate()
	}
	invalidAt(t, mem(func(c *contract.MemoryConfig) { c.Mode = "x" }), "mode")
	invalidAt(t, mem(func(c *contract.MemoryConfig) { c.ThresholdBp = 4999 }), "thresholdBp")
	invalidAt(t, mem(func(c *contract.MemoryConfig) { c.ThresholdBp = 10001 }), "thresholdBp")
	invalidAt(t, mem(func(c *contract.MemoryConfig) { c.MaxHintsPerSession = 0 }), "maxHintsPerSession")
	invalidAt(t, mem(func(c *contract.MemoryConfig) { c.MaxHintsPerSession = 4 }), "maxHintsPerSession")
	invalidAt(t, mem(func(c *contract.MemoryConfig) { c.Model = strings.Repeat("m", 129) }), "model")
	invalidAt(t, mem(func(c *contract.MemoryConfig) { c.EmbedModel = strings.Repeat("m", 129) }), "embedModel")
}

func TestTrimPatchMergesAndReportsTouchedFields(t *testing.T) {
	var p contract.TrimConfigPatch
	if err := json.Unmarshal([]byte(`{"mode":"lossless","stages":{"schema_trim":true},"neverTrimTools":["bash"]}`), &p); err != nil {
		t.Fatal(err)
	}
	base := contract.DefaultTrimConfig()
	got := p.Apply(base)
	if got.Mode != contract.TrimModeLossless || !got.Stages[contract.StageSchemaTrim] || !got.Stages[contract.StageDupLines] || !reflect.DeepEqual(got.NeverTrimTools, []string{"bash"}) {
		t.Fatalf("applied = %+v", got)
	}
	if base.Stages[contract.StageSchemaTrim] {
		t.Fatal("Apply must not mutate the base config's stages")
	}
	if got := p.Touched(); !reflect.DeepEqual(got, []string{"mode", "neverTrimTools", "stages"}) {
		t.Fatalf("touched = %v", got)
	}
	if got := (contract.TrimConfigPatch{}).Touched(); got == nil || len(got) != 0 {
		t.Fatalf("an empty patch touches nothing (and is a non-nil list), got %#v", got)
	}
	bad := contract.TrimConfigPatch{Stages: map[contract.TrimStage]bool{"bogus": true}}
	invalidAt(t, bad.Apply(base).Validate(), "stages.bogus")
}

func TestRouterAndMemoryPatches(t *testing.T) {
	thr, zero := 3000, 0
	rp := contract.RouterConfigPatch{Mode: new(contract.RouterModeConservative), ThresholdBp: &thr}
	c := rp.Apply(contract.DefaultRouterConfig())
	if c.Mode != contract.RouterModeConservative || c.ThresholdBp == nil || *c.ThresholdBp != 3000 {
		t.Fatalf("applied = %+v", c)
	}
	c = contract.RouterConfigPatch{ThresholdBp: &zero}.Apply(c)
	if c.ThresholdBp != nil {
		t.Fatal("thresholdBp 0 resets to the mode default")
	}
	pairs := []contract.RoutePair{{Provider: "openai", From: "gpt-5", To: "gpt-5-mini"}}
	c = contract.RouterConfigPatch{Map: &pairs}.Apply(c)
	pairs[0].To = "mutated"
	if len(c.Map) != 1 || c.Map[0].To != "gpt-5-mini" {
		t.Fatalf("the patch's map must be copied, got %+v", c.Map)
	}
	c = contract.RouterConfigPatch{Map: &[]contract.RoutePair{}}.Apply(c)
	if c.Map == nil || len(c.Map) != 0 {
		t.Fatalf("map [] stores an empty workspace map, got %#v", c.Map)
	}
	c = contract.RouterConfigPatch{ResetMap: new(true), Map: &pairs}.Apply(c)
	if c.Map != nil {
		t.Fatalf("resetMap returns to the defaults, got %#v", c.Map)
	}
	if got := (contract.RouterConfigPatch{Mode: rp.Mode, ResetMap: new(true)}).Touched(); !reflect.DeepEqual(got, []string{"mode", "map"}) {
		t.Fatalf("router touched = %v", got)
	}
	mp := contract.MemoryConfigPatch{Mode: new(contract.MemoryModeHint), MaxHintsPerSession: new(2), Model: new("m")}
	m := mp.Apply(contract.DefaultMemoryConfig())
	if m.Mode != contract.MemoryModeHint || m.MaxHintsPerSession != 2 || m.Model != "m" || m.ThresholdBp != 8200 {
		t.Fatalf("memory applied = %+v", m)
	}
	if got := mp.Touched(); !reflect.DeepEqual(got, []string{"mode", "maxHintsPerSession", "model"}) {
		t.Fatalf("memory touched = %v", got)
	}
	invalidAt(t, contract.MemoryConfigPatch{ThresholdBp: new(100)}.Apply(m).Validate(), "thresholdBp")
}
