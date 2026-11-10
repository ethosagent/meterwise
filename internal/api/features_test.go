package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
)

const missingWS = "00000000-0000-0000-0000-00000000dead"

// With no feature port wired: the settings still answer from the stored config, everything that reads through a
// port is 501 not_available, and the router lists no pairs.
func TestFeatureRoutesWithoutPorts(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	base := "/api/workspaces/" + ws

	if tc := decodeAs[contract.TrimConfig](t, s.must(t, admin.tok, "GET", base+"/trim", "", 200)); tc.Mode != contract.TrimModeObserve {
		t.Errorf("default trim = %+v", tc)
	}
	raw := string(s.must(t, admin.tok, "GET", base+"/router", "", 200))
	for _, want := range []string{`"defaults":[]`, `"suggestions":[]`, `"effective":[]`, `"mode":"off"`} {
		if !strings.Contains(raw, want) {
			t.Errorf("router settings lack %s: %s", want, raw)
		}
	}
	mem := decodeAs[contract.MemorySettings](t, s.must(t, admin.tok, "GET", base+"/memory", "", 200))
	if mem.Enabled || mem.DisabledReason == nil || *mem.DisabledReason != contract.MemoryDisabledNoKey || mem.Config.Mode != contract.MemoryModeOff {
		t.Errorf("memory without a port = %+v", mem)
	}
	for _, r := range []struct{ method, path, body string }{
		{"GET", "/trim/stats", ""}, {"GET", "/calls/c1/trim", ""}, {"GET", "/router/stats", ""},
		{"POST", "/calls/c1/router-feedback", `{"verdict":"worse"}`}, {"GET", "/memory/stats", ""}, {"GET", "/traces", ""},
		{"GET", "/traces/t1", ""}, {"DELETE", "/traces", ""}, {"POST", "/memory-hits/h1/feedback", `{"feedback":"helpful"}`},
	} {
		code, b := s.call(t, r.method, base+r.path, admin.tok, r.body)
		wantErr(t, code, b, http.StatusNotImplemented, contract.CodeNotAvailable)
	}
}

func TestFeatureSettingsPatchValidationAndAudit(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	base := "/api/workspaces/" + ws

	got := decodeAs[contract.TrimConfig](t, s.must(t, admin.tok, "PATCH", base+"/trim", `{"mode":"lossless","maxFoldPct":80}`, 200))
	if got.Mode != contract.TrimModeLossless || got.MaxFoldPct != 80 || got.MinGainPct != 2 {
		t.Fatalf("trim patch = %+v", got)
	}
	rs := decodeAs[contract.RouterSettings](t, s.must(t, admin.tok, "PATCH", base+"/router",
		`{"mode":"observe","map":[{"provider":"openai","from":"gpt-4.1","to":"gpt-4.1-mini"}]}`, 200))
	if rs.Config.Mode != contract.RouterModeObserve || len(rs.Config.Map) != 1 {
		t.Fatalf("router patch = %+v", rs)
	}
	ms := decodeAs[contract.MemorySettings](t, s.must(t, admin.tok, "PATCH", base+"/memory", `{"mode":"observe","thresholdBp":9000}`, 200))
	if ms.Config.Mode != contract.MemoryModeObserve || ms.Config.ThresholdBp != 9000 {
		t.Fatalf("memory patch = %+v", ms)
	}
	// the three settings are stored apart: each GET returns its own, and the workspace PATCH keeps all of them
	s.must(t, admin.tok, "PATCH", "/api/workspaces/"+ws, `{"settings":{"theme":"x","trim":{"mode":"off"},"router":{"mode":"aggressive"},"memory":{"mode":"hint"}}}`, 200)
	if decodeAs[contract.TrimConfig](t, s.must(t, admin.tok, "GET", base+"/trim", "", 200)).Mode != contract.TrimModeLossless ||
		decodeAs[contract.RouterSettings](t, s.must(t, admin.tok, "GET", base+"/router", "", 200)).Config.Mode != contract.RouterModeObserve ||
		decodeAs[contract.MemorySettings](t, s.must(t, admin.tok, "GET", base+"/memory", "", 200)).Config.Mode != contract.MemoryModeObserve {
		t.Fatal("a workspace settings PATCH changed a saver's settings")
	}

	for _, c := range []struct{ path, body, field string }{
		{"/trim", `{"mode":"panic"}`, "mode"}, {"/trim", `{"maxFoldPct":5}`, "maxFoldPct"}, {"/trim", `{"stages":{"nope":true}}`, "stages"},
		{"/router", `{"mode":"yolo"}`, "mode"}, {"/router", `{"thresholdBp":20000}`, "thresholdBp"},
		{"/router", `{"map":[{"provider":"openai","from":"a","to":"a"}]}`, "map[0].to"},
		{"/memory", `{"thresholdBp":100}`, "thresholdBp"}, {"/memory", `{"maxHintsPerSession":9}`, "maxHintsPerSession"},
	} {
		code, b := s.call(t, "PATCH", base+c.path, admin.tok, c.body)
		wantErr(t, code, b, 422, contract.CodeValidationFailed)
		if !strings.Contains(string(b), c.field) {
			t.Errorf("%s %s: the error does not name %s: %s", c.path, c.body, c.field, b)
		}
	}
	for _, p := range []string{"/trim", "/router", "/memory"} {
		code, b := s.call(t, "PATCH", base+p, admin.tok, `{"nope":1}`)
		wantErr(t, code, b, 422, contract.CodeValidationFailed)
	}
	for action, meta := range map[string]string{
		"workspace.trim_update":   `metadata->>'mode' = 'lossless' AND metadata->'fields' = '["mode","maxFoldPct"]'::jsonb`,
		"workspace.router_update": `metadata->>'mode' = 'observe' AND (metadata->>'pairs')::int = 1`,
		"workspace.memory_update": `metadata->>'mode' = 'observe' AND metadata->'fields' = '["mode","thresholdBp"]'::jsonb`,
	} {
		if n := s.count(t, `SELECT count(*) FROM audit_log WHERE action = $1 AND workspace_id = $2::uuid AND `+meta, action, ws); n != 1 {
			t.Errorf("%s audit rows = %d, want 1 (rejected patches write none)", action, n)
		}
	}
	if n := s.count(t, `SELECT count(*) FROM audit_log WHERE action IN ('workspace.trim_update','workspace.router_update','workspace.memory_update')`); n != 3 {
		t.Errorf("saver audit rows = %d, want 3", n)
	}
}

func TestFeatureRoutesPermissionsAndWorkspaceScope(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	member := s.invited(t, admin.tok, "user_member", contract.RoleMember)
	base := "/api/workspaces/" + ws

	for _, p := range []string{"/trim", "/router", "/memory"} {
		s.must(t, member.tok, "GET", base+p, "", 200) // monitor_config:read is a member default
		code, b := s.call(t, "PATCH", base+p, member.tok, `{"mode":"off"}`)
		wantErr(t, code, b, 403, contract.CodeForbidden) // monitor_config:write is not
		code, b = s.call(t, "GET", base+p, "", "")
		wantErr(t, code, b, 401, contract.CodeUnauthenticated)
		for _, bad := range []string{missingWS, "not-a-uuid"} {
			code, b = s.call(t, "GET", "/api/workspaces/"+bad+p, admin.tok, "")
			wantErr(t, code, b, 404, contract.CodeNotFound)
		}
	}
	code, b := s.call(t, "DELETE", base+"/traces", member.tok, "")
	wantErr(t, code, b, 403, contract.CodeForbidden) // forgetting needs workspace:manage
}

// The fake ports record what the handlers pass and answer fixed values.
type fakeTrim struct {
	ws, call string
	rng      contract.SavingsRange
}

func (f *fakeTrim) Stats(_ context.Context, ws string, r contract.SavingsRange) (contract.TrimStats, error) {
	f.ws, f.rng = ws, r
	return contract.TrimStats{Range: r.Range, Calls: 5}, nil
}

func (f *fakeTrim) Preview(_ context.Context, ws, id string) (contract.TrimPreview, error) {
	f.ws, f.call = ws, id
	if id == "nope" {
		return contract.TrimPreview{}, contract.NewError(contract.CodeNotFound, "not found")
	}
	return contract.TrimPreview{CallID: id, BodyBefore: 900, BodyAfter: 300}, nil
}

type fakeRouter struct {
	user, call string
	verdict    *string
}

var pairGPT41 = contract.RoutePair{Provider: "openai", From: "gpt-4.1", To: "gpt-4.1-mini"}

func (*fakeRouter) Defaults(context.Context) ([]contract.RoutePair, error) {
	return []contract.RoutePair{pairGPT41}, nil
}

func (*fakeRouter) Suggestions(context.Context, string) ([]contract.RoutePair, error) {
	return nil, nil
}

func (*fakeRouter) Stats(_ context.Context, _ string, r contract.SavingsRange) (contract.RouterStats, error) {
	return contract.RouterStats{Range: r.Range, Calls: 7}, nil
}

func (f *fakeRouter) SetFeedback(_ context.Context, _, id, user string, v *string) (contract.RouterFeedbackView, error) {
	f.call, f.user, f.verdict = id, user, v
	return contract.RouterFeedbackView{CallID: id, Verdict: v}, nil
}

type fakeMemory struct {
	forgot   int
	hit      string
	feedback *string
}

func (*fakeMemory) Status(context.Context, string) (bool, string, error) { return true, "", nil }

func (*fakeMemory) Stats(_ context.Context, _ string, r contract.SavingsRange) (contract.MemoryStats, error) {
	return contract.MemoryStats{Range: r.Range, Traces: 3}, nil
}

func (*fakeMemory) Traces(context.Context, string, string, int) (contract.TraceList, error) {
	return contract.TraceList{Items: []contract.TraceSummary{{ID: "t1", TaskSummary: "fix the build", Files: []string{}}}}, nil
}

func (*fakeMemory) Trace(_ context.Context, _, id string) (contract.TraceDetail, error) {
	if id == "nope" {
		return contract.TraceDetail{}, contract.NewError(contract.CodeNotFound, "not found")
	}
	return contract.TraceDetail{TraceSummary: contract.TraceSummary{ID: id}}, nil
}

func (f *fakeMemory) Forget(context.Context, string) (int64, error) {
	f.forgot++
	return 4, nil
}

func (f *fakeMemory) SetHitFeedback(_ context.Context, _, id string, fb *string) (contract.MemoryHitView, error) {
	f.hit, f.feedback = id, fb
	return contract.MemoryHitView{ID: id, Feedback: fb}, nil
}

func TestFeatureRoutesThroughPorts(t *testing.T) {
	tr, rr, mem := &fakeTrim{}, &fakeRouter{}, &fakeMemory{}
	s := newStackWith(t, func(d *api.Deps) { d.Trim, d.Router, d.Memory = tr, rr, mem })
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	base := "/api/workspaces/" + ws
	s.must(t, admin.tok, "PATCH", base+"/trim", `{"mode":"lossless"}`, 200)

	if ts := decodeAs[contract.TrimStats](t, s.must(t, admin.tok, "GET", base+"/trim/stats?range=30d", "", 200)); ts.Calls != 5 || ts.Mode != contract.TrimModeLossless || ts.Range != "30d" {
		t.Errorf("trim stats = %+v (the handler fills the mode)", ts)
	}
	if tr.ws != ws || tr.rng.Days() != 30 {
		t.Errorf("the port saw workspace %q and %d days", tr.ws, tr.rng.Days())
	}
	code, b := s.call(t, "GET", base+"/trim/stats?range=1y", admin.tok, "")
	wantErr(t, code, b, 422, contract.CodeValidationFailed)
	if p := decodeAs[contract.TrimPreview](t, s.must(t, admin.tok, "GET", base+"/calls/c9/trim", "", 200)); p.CallID != "c9" || p.BodyAfter != 300 {
		t.Errorf("preview = %+v", p)
	}
	code, b = s.call(t, "GET", base+"/calls/nope/trim", admin.tok, "")
	wantErr(t, code, b, 404, contract.CodeNotFound)

	// effective pairs: the defaults until the workspace stores its own map
	if st := decodeAs[contract.RouterSettings](t, s.must(t, admin.tok, "GET", base+"/router", "", 200)); len(st.Defaults) != 1 || len(st.Effective) != 1 || st.Suggestions == nil {
		t.Errorf("router settings = %+v", st)
	}
	own := decodeAs[contract.RouterSettings](t, s.must(t, admin.tok, "PATCH", base+"/router", `{"map":[{"provider":"openai","from":"gpt-5","to":"gpt-5-mini"}]}`, 200))
	if len(own.Defaults) != 1 || len(own.Effective) != 1 || own.Effective[0].From != "gpt-5" {
		t.Errorf("a workspace map replaces the defaults in effect: %+v", own)
	}
	if st := decodeAs[contract.RouterStats](t, s.must(t, admin.tok, "GET", base+"/router/stats", "", 200)); st.Calls != 7 || st.Mode != contract.RouterModeOff {
		t.Errorf("router stats = %+v", st)
	}
	fb := base + "/calls/c9/router-feedback"
	if v := decodeAs[contract.RouterFeedbackView](t, s.must(t, admin.tok, "POST", fb, `{"verdict":"worse"}`, 200)); v.Verdict == nil || *v.Verdict != "worse" || v.CallID != "c9" {
		t.Errorf("feedback = %+v", v)
	}
	if rr.user == "" {
		t.Error("the port must be told who gave the feedback")
	}
	s.must(t, admin.tok, "POST", fb, `{"verdict":null}`, 200)
	if rr.verdict != nil {
		t.Error("null clears the verdict")
	}
	code, b = s.call(t, "POST", fb, admin.tok, `{"verdict":"meh"}`)
	wantErr(t, code, b, 422, contract.CodeValidationFailed)
	if n := s.count(t, `SELECT count(*) FROM audit_log WHERE action = 'router.feedback' AND resource_id = 'c9' AND workspace_id = $1::uuid`, ws); n != 2 {
		t.Errorf("router.feedback audit rows = %d, want 2 (the rejected one writes none)", n)
	}

	if ms := decodeAs[contract.MemoryStats](t, s.must(t, admin.tok, "GET", base+"/memory/stats", "", 200)); ms.Traces != 3 || ms.Mode != contract.MemoryModeOff {
		t.Errorf("memory stats = %+v", ms)
	}
	if on := decodeAs[contract.MemorySettings](t, s.must(t, admin.tok, "GET", base+"/memory", "", 200)); !on.Enabled || on.DisabledReason != nil {
		t.Errorf("memory settings = %+v", on)
	}
	if tl := decodeAs[contract.TraceList](t, s.must(t, admin.tok, "GET", base+"/traces?limit=10", "", 200)); len(tl.Items) != 1 {
		t.Errorf("traces = %+v", tl)
	}
	code, b = s.call(t, "GET", base+"/traces?limit=0", admin.tok, "")
	wantErr(t, code, b, 422, contract.CodeValidationFailed)
	s.must(t, admin.tok, "GET", base+"/traces/t1", "", 200)
	code, b = s.call(t, "GET", base+"/traces/nope", admin.tok, "")
	wantErr(t, code, b, 404, contract.CodeNotFound)
	hit := base + "/memory-hits/h1/feedback"
	if v := decodeAs[contract.MemoryHitView](t, s.must(t, admin.tok, "POST", hit, `{"feedback":"helpful"}`, 200)); v.Feedback == nil || *v.Feedback != "helpful" {
		t.Errorf("hit feedback = %+v", v)
	}
	code, b = s.call(t, "POST", hit, admin.tok, `{"feedback":"great"}`)
	wantErr(t, code, b, 422, contract.CodeValidationFailed)
	if body := s.must(t, admin.tok, "DELETE", base+"/traces", "", 204); len(body) != 0 || mem.forgot != 1 {
		t.Errorf("forget body %q, forgot %d", body, mem.forgot)
	}
	if n := s.count(t, `SELECT count(*) FROM audit_log WHERE action = 'memory.forget' AND (metadata->>'count')::int = 4 AND workspace_id = $1::uuid`, ws); n != 1 {
		t.Errorf("memory.forget audit rows = %d, want 1", n)
	}
	if n := s.count(t, `SELECT count(*) FROM audit_log WHERE action = 'memory_hit.feedback' AND resource_id = 'h1'`); n != 1 {
		t.Errorf("memory_hit.feedback audit rows = %d, want 1", n)
	}
}
