package api_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

func monitorVerdict(m contract.MonitorName, action contract.MonitorMode, conf contract.Confidence, usd contract.MicroUSD) contract.PricedVerdict {
	return contract.PricedVerdict{
		MonitorVerdict: contract.MonitorVerdict{Monitor: m, Mode: contract.ModeNote, Action: action, Evidence: contract.MonitorEvidence{Tool: "read_file", ArgsHash: "ab12cd34", Count: 3, Threshold: 2}},
		EstTokens:      1000, EstUSD: usd, Confidence: conf,
	}
}

func TestMonitorConfigGetPatchAndValidation(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	path := "/api/workspaces/" + ws + "/monitors"

	def := decodeAs[contract.MonitorConfig](t, s.must(t, admin.tok, "GET", path, "", 200))
	if def != contract.DefaultMonitorConfig() {
		t.Fatalf("a fresh workspace must read the defaults, got %+v", def)
	}
	got := decodeAs[contract.MonitorConfig](t, s.must(t, admin.tok, "PATCH", path,
		`{"step_budget":{"mode":"note","maxSteps":12},"duplicate_tool_call":{"mode":"block","abab":true}}`, 200))
	want := contract.DefaultMonitorConfig()
	want.StepBudget.Mode, want.StepBudget.MaxSteps = contract.ModeNote, 12
	want.DuplicateToolCall.Mode, want.DuplicateToolCall.ABAB = contract.ModeBlock, true
	if got != want {
		t.Fatalf("patched = %+v, want %+v", got, want)
	}
	// a second partial patch keeps the first one's fields
	again := decodeAs[contract.MonitorConfig](t, s.must(t, admin.tok, "PATCH", path, `{"step_budget":{"maxSteps":20}}`, 200))
	want.StepBudget.MaxSteps = 20
	if again != want || decodeAs[contract.MonitorConfig](t, s.must(t, admin.tok, "GET", path, "", 200)) != want {
		t.Fatalf("second patch = %+v, want %+v", again, want)
	}

	// 422 validation with the field path, nothing written
	for body, field := range map[string]string{
		`{"step_budget":{"maxSteps":0}}`:           "step_budget.maxSteps",
		`{"duplicate_tool_call":{"mode":"panic"}}`: "duplicate_tool_call.mode",
		`{"no_progress":{"similarityPct":10}}`:     "no_progress.similarityPct",
		`{"second_guessing":{"minPhrases":99}}`:    "second_guessing.minPhrases",
		`{"repeated_read":{"blockAfter":-1}}`:      "repeated_read.blockAfter",
	} {
		code, b := s.call(t, "PATCH", path, admin.tok, body)
		wantErr(t, code, b, 422, contract.CodeValidationFailed)
		if !strings.Contains(string(b), field) {
			t.Errorf("%s: the error does not name %s: %s", body, field, b)
		}
	}
	for _, body := range []string{`{"nope":{}}`, `{"step_budget":{"nope":1}}`, `{"step_budget":{"maxSteps":"x"}}`, `not json`} {
		code, b := s.call(t, "PATCH", path, admin.tok, body)
		wantErr(t, code, b, 422, contract.CodeValidationFailed)
	}
	if decodeAs[contract.MonitorConfig](t, s.must(t, admin.tok, "GET", path, "", 200)) != want {
		t.Fatal("a rejected patch changed the config")
	}

	// audit: workspace.monitors_update with the touched monitors and their modes, no thresholds
	if n := s.count(t, `SELECT count(*) FROM audit_log WHERE action = 'workspace.monitors_update' AND workspace_id = $1::uuid
	                    AND metadata->'monitors' = '["duplicate_tool_call","step_budget"]'::jsonb AND metadata->'modes'->>'duplicate_tool_call' = 'block'`, ws); n != 1 {
		t.Fatalf("monitors audit rows = %d, want 1", n)
	}
	if n := s.count(t, `SELECT count(*) FROM audit_log WHERE action = 'workspace.monitors_update'`); n != 2 {
		t.Fatalf("audit rows = %d, want 2 (rejected patches write none)", n)
	}
}

func TestMonitorRoutesPermissionsAndWorkspaceScope(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	member := s.invited(t, admin.tok, "user_member", contract.RoleMember)
	path := "/api/workspaces/" + ws + "/monitors"

	s.must(t, member.tok, "GET", path, "", 200) // monitor_config:read is a member default
	code, b := s.call(t, "PATCH", path, member.tok, `{"step_budget":{"mode":"note"}}`)
	wantErr(t, code, b, 403, contract.CodeForbidden) // monitor_config:write is not
	for _, p := range []string{"/api/workspaces/00000000-0000-0000-0000-00000000dead/monitors", "/api/workspaces/not-a-uuid/monitors/stats"} {
		code, b := s.call(t, "GET", p, admin.tok, "")
		wantErr(t, code, b, 404, contract.CodeNotFound)
	}
	code, b = s.call(t, "GET", path, "", "")
	wantErr(t, code, b, 401, contract.CodeUnauthenticated)
}

func TestWorkspacePatchKeepsMonitorSettings(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	s.must(t, admin.tok, "PATCH", "/api/workspaces/"+ws+"/monitors", `{"step_budget":{"mode":"note","maxSteps":7}}`, 200)
	// the settings replacement brings its own (unvalidated) monitors key: dropped, the stored config stays
	s.must(t, admin.tok, "PATCH", "/api/workspaces/"+ws, `{"settings":{"theme":"x","monitors":{"step_budget":{"mode":"block","maxSteps":99999}}}}`, 200)
	cfg := decodeAs[contract.MonitorConfig](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/monitors", "", 200))
	if cfg.StepBudget.Mode != contract.ModeNote || cfg.StepBudget.MaxSteps != 7 {
		t.Fatalf("monitors after a workspace PATCH = %+v", cfg.StepBudget)
	}
	if n := s.count(t, `SELECT count(*) FROM workspaces WHERE id = $1::uuid AND settings->>'theme' = 'x'`, ws); n != 1 {
		t.Fatal("the other settings keys were not replaced as before")
	}
}

func TestMonitorFeedbackRoute(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	other := s.newWorkspace(t, admin.tok, "Other")
	now := time.Now().UTC()
	a := callRec(ws, "a1", "sess-a", 1, now.Add(-time.Hour), nil)
	a.Verdicts = []contract.PricedVerdict{monitorVerdict(contract.MonitorRepeatedRead, contract.ModeNote, contract.ConfidenceLikely, 400_000)}
	s.seedCalls(t, a)

	detail := decodeAs[contract.SessionDetail](t, s.must(t, admin.tok, "GET", "/api/sessions/sess-a?workspaceId="+ws, "", 200))
	if len(detail.MonitorEvents) != 1 {
		t.Fatalf("detail events = %+v", detail.MonitorEvents)
	}
	ev := detail.MonitorEvents[0]
	if ev.Monitor != contract.MonitorRepeatedRead || ev.Action != contract.ModeNote || ev.EstSavedUsd != 400_000 || ev.Feedback != nil || ev.CallID != "a1" ||
		ev.Evidence.Tool != "read_file" || ev.Confidence != contract.ConfidenceLikely || ev.EstSavedTokens != 1000 {
		t.Fatalf("event = %+v", ev)
	}
	raw := s.must(t, admin.tok, "GET", "/api/sessions/sess-a?workspaceId="+ws, "", 200)
	if !strings.Contains(string(raw), `"estSavedUsd":"0.400000"`) || !strings.Contains(string(raw), `"feedback":null`) || !strings.Contains(string(raw), `"id":"`+ev.ID+`"`) {
		t.Errorf("wire form: %s", raw)
	}
	fb := "/api/workspaces/" + ws + "/monitor-events/" + ev.ID + "/feedback"
	got := decodeAs[contract.MonitorEventView](t, s.must(t, admin.tok, "POST", fb, `{"feedback":"wrong"}`, 200))
	if got.Feedback == nil || *got.Feedback != "wrong" || got.ID != ev.ID {
		t.Fatalf("feedback = %+v", got)
	}
	if cleared := decodeAs[contract.MonitorEventView](t, s.must(t, admin.tok, "POST", fb, `{"feedback":null}`, 200)); cleared.Feedback != nil {
		t.Fatalf("cleared = %+v", cleared)
	}
	s.must(t, admin.tok, "POST", fb, `{"feedback":"correct"}`, 200)

	for body, status := range map[string]int{`{"feedback":"maybe"}`: 422, `{"feedback":1}`: 422, `{"extra":1}`: 422, `nope`: 422} {
		code, b := s.call(t, "POST", fb, admin.tok, body)
		wantErr(t, code, b, status, contract.CodeValidationFailed)
	}
	// a cross-workspace event id, an unknown id and a malformed id are all 404
	for _, p := range []string{
		"/api/workspaces/" + other + "/monitor-events/" + ev.ID + "/feedback",
		"/api/workspaces/" + ws + "/monitor-events/999999/feedback",
		"/api/workspaces/" + ws + "/monitor-events/abc/feedback",
		"/api/workspaces/" + ws + "/monitor-events/-3/feedback",
	} {
		code, b := s.call(t, "POST", p, admin.tok, `{"feedback":"wrong"}`)
		wantErr(t, code, b, 404, contract.CodeNotFound)
	}
	if n := s.count(t, `SELECT count(*) FROM monitor_events WHERE feedback = 'correct'`); n != 1 {
		t.Fatalf("a refused request changed feedback: %d correct rows", n)
	}
	// audit rows: three successful changes, with the monitor and the new value, never the other workspace
	if n := s.count(t, `SELECT count(*) FROM audit_log WHERE action = 'monitor_event.feedback' AND resource_type = 'monitor_event'
	                    AND resource_id = $1 AND workspace_id = $2::uuid AND metadata->>'monitor' = 'repeated_read'`, ev.ID, ws); n != 3 {
		t.Fatalf("feedback audit rows = %d, want 3", n)
	}
	if n := s.count(t, `SELECT count(*) FROM audit_log WHERE action = 'monitor_event.feedback' AND metadata->'feedback' = 'null'::jsonb`); n != 1 {
		t.Fatal("the cleared feedback must audit as null")
	}
}

func TestMonitorStatsRoute(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	now := time.Now().UTC()
	c1 := callRec(ws, "c1", "s1", 1, now.Add(-2*time.Hour), nil)
	c1.Verdicts = []contract.PricedVerdict{
		monitorVerdict(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ConfidenceCertain, 3_000_000),
		monitorVerdict(contract.MonitorRepeatedRead, contract.ModeNote, contract.ConfidenceLikely, 1_000_000),
	}
	c2 := callRec(ws, "c2", "s1", 2, now.Add(-time.Hour), nil)
	c2.Verdicts = []contract.PricedVerdict{
		monitorVerdict(contract.MonitorDuplicateToolCall, contract.ModeObserve, contract.ConfidenceCertain, 5_000_000),
		monitorVerdict(contract.MonitorRepeatedRead, contract.ModeNote, contract.ConfidenceLikely, 500_000),
	}
	old := callRec(ws, "old", "s0", 1, now.AddDate(0, 0, -20), nil) // inside 30d, outside 7d
	old.Verdicts = []contract.PricedVerdict{monitorVerdict(contract.MonitorNoProgress, contract.ModeBlock, contract.ConfidenceCertain, 7_000_000)}
	s.seedCalls(t, c1, c2, old)
	s.must(t, admin.tok, "PATCH", "/api/workspaces/"+ws+"/monitors", `{"duplicate_tool_call":{"mode":"block"}}`, 200)

	evs := decodeAs[contract.SessionDetail](t, s.must(t, admin.tok, "GET", "/api/sessions/s1?workspaceId="+ws, "", 200)).MonitorEvents
	for _, e := range evs {
		if e.Monitor == contract.MonitorRepeatedRead {
			fbv := "correct"
			if e.CallID == "c2" {
				fbv = "wrong"
			}
			s.must(t, admin.tok, "POST", "/api/workspaces/"+ws+"/monitor-events/"+e.ID+"/feedback", `{"feedback":"`+fbv+`"}`, 200)
		}
	}

	st := decodeAs[contract.MonitorStatsView](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/monitors/stats", "", 200))
	if st.Range != "7d" || len(st.Monitors) != 6 || st.SavedUsd != 3_000_000 || st.LikelySavedUsd != 1_500_000 || st.WouldSaveUsd != 5_000_000 {
		t.Fatalf("stats 7d = %+v", st)
	}
	for i, n := range contract.MonitorNames() {
		if st.Monitors[i].Monitor != n {
			t.Fatalf("row %d = %s, want %s", i, st.Monitors[i].Monitor, n)
		}
	}
	dup, rr, np := st.Monitors[0], st.Monitors[1], st.Monitors[2]
	if dup.Mode != contract.ModeBlock || dup.Fires != 2 || dup.Blocked != 1 || dup.Observed != 1 || dup.SavedUsd != 3_000_000 || dup.WouldSaveUsd != 5_000_000 || dup.FalsePositiveRate != nil {
		t.Errorf("duplicate_tool_call = %+v", dup)
	}
	if rr.Mode != contract.ModeObserve || rr.Noted != 2 || rr.LikelySavedUsd != 1_500_000 || rr.FeedbackCorrect != 1 || rr.FeedbackWrong != 1 || rr.FalsePositiveRate == nil || *rr.FalsePositiveRate != 0.5 {
		t.Errorf("repeated_read = %+v", rr)
	}
	if np.Fires != 0 {
		t.Errorf("no_progress is outside 7d: %+v", np)
	}
	raw := s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/monitors/stats", "", 200)
	if !strings.Contains(string(raw), `"falsePositiveRate":null`) || !strings.Contains(string(raw), `"savedUsd":"3.000000"`) || strings.Contains(string(raw), "4.500000") {
		t.Errorf("wire form (saved and likely are never summed): %s", raw)
	}
	st30 := decodeAs[contract.MonitorStatsView](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/monitors/stats?range=30d", "", 200))
	if st30.Range != "30d" || st30.SavedUsd != 10_000_000 {
		t.Fatalf("stats 30d = %+v", st30)
	}
	code, b := s.call(t, "GET", "/api/workspaces/"+ws+"/monitors/stats?range=1y", admin.tok, "")
	wantErr(t, code, b, 422, contract.CodeValidationFailed)

	// the workspace stats carry the same two numbers, separate
	ws7 := decodeAs[contract.StatsView](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/stats", "", 200))
	if ws7.SavedUsd != 3_000_000 || ws7.LikelySavedUsd != 1_500_000 {
		t.Fatalf("stats view saved/likely = %v/%v", ws7.SavedUsd, ws7.LikelySavedUsd)
	}
	// the session list carries each session's own pair
	list := decodeAs[contract.SessionList](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/sessions", "", 200))
	for _, it := range list.Items {
		if it.ID == "s1" && (it.SavedUsd != 3_000_000 || it.LikelySavedUsd != 1_500_000) {
			t.Errorf("session s1 = %+v", it)
		}
	}
	// another workspace sees none of it
	empty := decodeAs[contract.MonitorStatsView](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+s.newWorkspace(t, admin.tok, "Empty")+"/monitors/stats", "", 200))
	if empty.SavedUsd != 0 || empty.Monitors[0].Fires != 0 || empty.Monitors[0].FalsePositiveRate != nil {
		t.Fatalf("empty = %+v", empty)
	}
}
