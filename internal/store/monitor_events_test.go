package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

func verdict(m contract.MonitorName, action contract.MonitorMode, conf contract.Confidence, tokens int64, usd contract.MicroUSD) contract.PricedVerdict {
	return contract.PricedVerdict{
		MonitorVerdict: contract.MonitorVerdict{
			Monitor: m, Mode: contract.ModeBlock, Action: action,
			Evidence: contract.MonitorEvidence{MatchedCalls: []int{1, 2}, Tool: "read", ArgsHash: "abcd", Count: 3, Threshold: 2},
		},
		EstTokens: tokens, EstUSD: usd, Confidence: conf,
	}
}

func count(t *testing.T, s *store.Store, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.Pool().QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Verdicts become monitor_events rows in the call's transaction, the session sums follow ruling R6
// (certain acted -> saved, likely acted -> likely, observe -> neither) and a replay writes nothing twice.
func TestWriteCallsWritesMonitorEventsAndSessionSums(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	c1 := rec(ws, "c1", "s1", 1, base, "ok", true, mu(10))
	c1.Blocked = true
	c1.Verdicts = []contract.PricedVerdict{
		verdict(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ConfidenceCertain, 1000, 3000),
		verdict(contract.MonitorStepBudget, contract.ModeNote, contract.ConfidenceNone, 0, 0),
	}
	c2 := rec(ws, "c2", "s1", 2, base.Add(time.Second), "ok", true, mu(10))
	c2.Verdicts = []contract.PricedVerdict{
		verdict(contract.MonitorRepeatedRead, contract.ModeNote, contract.ConfidenceLikely, 400, 1200),
		verdict(contract.MonitorNoProgress, contract.ModeObserve, contract.ConfidenceLikely, 900, 9000), // would-save
	}
	c3 := rec(ws, "c3", "s2", 1, base, "ok", true, mu(10)) // no verdicts: a session with zero sums
	batch := []contract.CallRecord{c1, c2, c3}
	if _, err := s.WriteCalls(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT count(*) FROM monitor_events WHERE workspace_id = $1`, ws); n != 4 {
		t.Fatalf("events = %d, want 4", n)
	}
	sess, _, _, err := s.Session(ctx, ws, "s1")
	if err != nil || sess.Saved != 3000 || sess.LikelySaved != 1200 {
		t.Fatalf("s1 saved=%v likely=%v err=%v; want 3000 and 1200 (observe in neither)", sess.Saved, sess.LikelySaved, err)
	}
	if s2, _, _, _ := s.Session(ctx, ws, "s2"); s2.Saved != 0 || s2.LikelySaved != 0 {
		t.Fatalf("s2 sums = %v %v, want 0", s2.Saved, s2.LikelySaved)
	}
	// replaying the batch inserts no call, no event and moves no sum
	if res, err := s.WriteCalls(ctx, batch); err != nil || res.Inserted != 0 {
		t.Fatalf("replay = %+v, %v", res, err)
	}
	if n := count(t, s, `SELECT count(*) FROM monitor_events WHERE workspace_id = $1`, ws); n != 4 {
		t.Fatalf("events after replay = %d, want 4", n)
	}
	if again, _, _, _ := s.Session(ctx, ws, "s1"); again.Saved != 3000 || again.LikelySaved != 1200 {
		t.Fatalf("sums moved on replay: %v %v", again.Saved, again.LikelySaved)
	}
	// the stored events carry the evidence and money; the session detail reads them in call order
	evs, err := s.SessionMonitorEvents(ctx, ws, "s1")
	if err != nil || len(evs) != 4 || evs[0].CallIndex != 1 || evs[3].CallIndex != 2 {
		t.Fatalf("events = %+v, %v", evs, err)
	}
	first := evs[0]
	if first.Monitor != contract.MonitorDuplicateToolCall || first.Action != contract.ModeBlock || first.EstSavedUSD != 3000 ||
		first.EstSavedToken != 1000 || first.Evidence.ArgsHash != "abcd" || first.Evidence.Count != 3 || first.Feedback != nil {
		t.Fatalf("event = %+v", first)
	}
	if other, err := s.SessionMonitorEvents(ctx, newWS(t, s, "B", "BBBBBBBBBBBBBBBBBBBBBB"), "s1"); err != nil || len(other) != 0 {
		t.Fatalf("another workspace saw %d events, %v", len(other), err)
	}
}

// Two verdicts of one monitor on one call (a bug upstream) cannot double-write: the unique key keeps one row.
// The unique key is the backstop for a monitor that reports twice for one call: one event, and the session
// sums count what was inserted (the RETURNING rows), so saved_usd keeps equalling SUM(monitor_events).
func TestMonitorEventUniqueKeyIsTheBackstop(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	c := rec(ws, "c1", "s1", 1, base, "ok", true, nil)
	certain := verdict(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ConfidenceCertain, 1, 7)
	likely := verdict(contract.MonitorRepeatedRead, contract.ModeNote, contract.ConfidenceLikely, 1, 3)
	c.Verdicts = []contract.PricedVerdict{certain, certain, likely, likely}
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{c}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT count(*) FROM monitor_events WHERE workspace_id = $1`, ws); n != 2 {
		t.Fatalf("events = %d, want 2", n)
	}
	for col, want := range map[string]int{"saved_usd": 7, "likely_saved_usd": 3} {
		if n := count(t, s, `SELECT (`+col+`*1000000)::int FROM sessions WHERE workspace_id = $1 AND id = 's1'`, ws); n != want {
			t.Errorf("sessions.%s = %d micro-USD, want %d (a duplicate verdict must not move the sums)", col, n, want)
		}
	}
}

func TestSetFeedbackScopesToTheWorkspace(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	a := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	b := newWS(t, s, "B", "BBBBBBBBBBBBBBBBBBBBBB")
	c := rec(a, "c1", "s1", 1, base, "ok", true, nil)
	c.Verdicts = []contract.PricedVerdict{verdict(contract.MonitorRepeatedRead, contract.ModeNote, contract.ConfidenceLikely, 1, 1)}
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{c}); err != nil {
		t.Fatal(err)
	}
	evs, _ := s.SessionMonitorEvents(ctx, a, "s1")
	id := evs[0].ID
	wrong := contract.FeedbackWrong
	if got, err := s.SetFeedback(ctx, a, id, &wrong); err != nil || got.Feedback == nil || *got.Feedback != "wrong" || got.ID != id {
		t.Fatalf("set = %+v, %v", got, err)
	}
	if got, err := s.SetFeedback(ctx, a, id, nil); err != nil || got.Feedback != nil {
		t.Fatalf("clear = %+v, %v", got, err)
	}
	for name, tc := range map[string]struct {
		ws string
		id int64
	}{"another workspace": {b, id}, "unknown id": {a, id + 999}, "malformed workspace": {"nope", id}} {
		if _, err := s.SetFeedback(ctx, tc.ws, tc.id, &wrong); code(err) != contract.CodeNotFound {
			t.Errorf("%s: err = %v, want not_found", name, err)
		}
	}
	if evs, _ := s.SessionMonitorEvents(ctx, a, "s1"); evs[0].Feedback != nil {
		t.Error("a refused feedback changed the event")
	}
}

func TestMonitorStatsPerMonitorZeroFilled(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	c1 := rec(ws, "c1", "s1", 1, base, "ok", true, nil)
	c1.Verdicts = []contract.PricedVerdict{
		verdict(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ConfidenceCertain, 1000, 3000),
		verdict(contract.MonitorRepeatedRead, contract.ModeNote, contract.ConfidenceLikely, 400, 1200),
	}
	c2 := rec(ws, "c2", "s1", 2, base.Add(time.Second), "ok", true, nil)
	c2.Verdicts = []contract.PricedVerdict{
		verdict(contract.MonitorDuplicateToolCall, contract.ModeObserve, contract.ConfidenceCertain, 500, 5000),
		verdict(contract.MonitorRepeatedRead, contract.ModeNote, contract.ConfidenceLikely, 100, 300),
	}
	old := rec(ws, "old", "s0", 1, base.AddDate(0, 0, -40), "ok", true, nil) // outside the window
	old.Verdicts = []contract.PricedVerdict{verdict(contract.MonitorNoProgress, contract.ModeBlock, contract.ConfidenceCertain, 1, 7)}
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{c1, c2, old}); err != nil {
		t.Fatal(err)
	}
	evs, _ := s.SessionMonitorEvents(ctx, ws, "s1")
	correct, wrong := contract.FeedbackCorrect, contract.FeedbackWrong
	seen := 0
	for _, e := range evs {
		if e.Monitor != contract.MonitorRepeatedRead {
			continue
		}
		fb := &correct
		if seen++; seen%2 == 0 {
			fb = &wrong
		}
		if _, err := s.SetFeedback(ctx, ws, e.ID, fb); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.MonitorStats(ctx, ws, base.Add(-time.Hour), base.Add(time.Hour))
	if err != nil || len(rows) != 6 {
		t.Fatalf("rows = %d, %v", len(rows), err)
	}
	for i, n := range contract.MonitorNames() {
		if rows[i].Monitor != n {
			t.Fatalf("row %d = %s, want %s (fixed order)", i, rows[i].Monitor, n)
		}
	}
	dup, rr, np := rows[0], rows[1], rows[2]
	if dup.Fires != 2 || dup.Blocked != 1 || dup.Observed != 1 || dup.Saved != 3000 || dup.WouldSave != 5000 || dup.Likely != 0 ||
		dup.SavedTokens != 1000 || dup.LikelySavedTokens != 0 || dup.WouldSaveTokens != 500 {
		t.Errorf("duplicate_tool_call = %+v", dup)
	}
	if rr.Fires != 2 || rr.Noted != 2 || rr.Likely != 1500 || rr.Saved != 0 || rr.FeedbackCorrect != 1 || rr.FeedbackWrong != 1 {
		t.Errorf("repeated_read = %+v", rr)
	}
	if np.Fires != 0 || np.Saved != 0 {
		t.Errorf("no_progress is outside the window and must be zero: %+v", np)
	}
	if r := contract.FalsePositiveRate(rr.FeedbackCorrect, rr.FeedbackWrong); r == nil || *r != 0.5 {
		t.Errorf("fp rate = %v", r)
	}
	st, err := s.Stats(ctx, ws, base.Add(-time.Hour), base.Add(time.Hour))
	if err != nil || st.Saved != 3000 || st.Likely != 1500 {
		t.Errorf("stats saved/likely = %v/%v, %v; want 3000/1500 (never summed, observe excluded)", st.Saved, st.Likely, err)
	}
}

// A settings replacement through UpdateWorkspace keeps settings.monitors, drops a "monitors" key the request brings
// (the monitors have their own validated route) and the monitors update keeps every other settings key.
func TestWorkspaceSettingsAndMonitorsStayApart(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	org := store.OrgID()
	cfg, err := s.UpdateWorkspaceMonitors(ctx, org, ws, func(c contract.MonitorConfig) (contract.MonitorConfig, error) {
		if c != contract.DefaultMonitorConfig() {
			t.Errorf("a fresh workspace must start at the defaults, got %+v", c)
		}
		c.StepBudget.Mode, c.StepBudget.MaxSteps = contract.ModeNote, 12
		return c, nil
	})
	if err != nil || cfg.StepBudget.MaxSteps != 12 {
		t.Fatalf("update = %+v, %v", cfg, err)
	}
	got, err := s.UpdateWorkspace(ctx, org, ws, nil, map[string]any{"theme": "x", "monitors": map[string]any{"step_budget": map[string]any{"mode": "block"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Settings["theme"] != "x" {
		t.Errorf("settings = %v, want theme kept", got.Settings)
	}
	if c := contract.MonitorConfigFrom(got.Settings["monitors"]); c.StepBudget.Mode != contract.ModeNote || c.StepBudget.MaxSteps != 12 {
		t.Errorf("monitors after a settings replace = %+v, want the stored ones", c)
	}
	if _, err := s.UpdateWorkspaceMonitors(ctx, org, ws, func(c contract.MonitorConfig) (contract.MonitorConfig, error) { return c, contract.Invalid("x", "bad") }); code(err) != contract.CodeValidationFailed {
		t.Errorf("an apply error must come back as is, got %v", err)
	}
	if w, _ := s.Workspace(ctx, org, ws); w.Settings["theme"] != "x" {
		t.Errorf("monitors update lost another key: %v", w.Settings)
	}
	if _, err := s.UpdateWorkspaceMonitors(ctx, org, "nope", nil); code(err) != contract.CodeNotFound {
		t.Errorf("malformed id: %v", err)
	}
	if _, err := s.UpdateWorkspaceMonitors(ctx, "00000000-0000-0000-0000-000000000000", ws, func(c contract.MonitorConfig) (contract.MonitorConfig, error) { return c, nil }); code(err) != contract.CodeNotFound {
		t.Errorf("another org: %v", err)
	}
}

// TestMonitorStatsTokenFieldsNeverSumCertainAndLikely: each token column counts exactly one class. A block
// (certain), a note (likely) and an observe (would-save) of one monitor land in three separate fields, and no
// field holds a total of two of them (non-negotiable 5).
func TestMonitorStatsTokenFieldsNeverSumCertainAndLikely(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "P", "PPPPPPPPPPPPPPPPPPPPPP")
	c1 := rec(ws, "p1", "s1", 1, base, "ok", true, nil)
	c1.Verdicts = []contract.PricedVerdict{verdict(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ConfidenceCertain, 1000, 3000)}
	c2 := rec(ws, "p2", "s1", 2, base.Add(time.Second), "ok", true, nil)
	c2.Verdicts = []contract.PricedVerdict{verdict(contract.MonitorDuplicateToolCall, contract.ModeNote, contract.ConfidenceLikely, 400, 1200)}
	c3 := rec(ws, "p3", "s1", 3, base.Add(2*time.Second), "ok", true, nil)
	c3.Verdicts = []contract.PricedVerdict{verdict(contract.MonitorDuplicateToolCall, contract.ModeObserve, contract.ConfidenceLikely, 70, 210)}
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{c1, c2, c3}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.MonitorStats(ctx, ws, base.Add(-time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	d := rows[0]
	if d.SavedTokens != 1000 || d.LikelySavedTokens != 400 || d.WouldSaveTokens != 70 {
		t.Errorf("tokens saved/likely/would = %d/%d/%d, want 1000/400/70 (one class each)", d.SavedTokens, d.LikelySavedTokens, d.WouldSaveTokens)
	}
	if d.Saved != 3000 || d.Likely != 1200 || d.WouldSave != 210 {
		t.Errorf("usd saved/likely/would = %v/%v/%v, want 3000/1200/210", d.Saved, d.Likely, d.WouldSave)
	}
}

// UpdateWorkspaceSetting writes one saver key, keeps every other key, accepts only trim/router/memory, and a
// settings replacement through UpdateWorkspace keeps all four validated keys.
func TestWorkspaceSaverSettingsStayApart(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	org := store.OrgID()
	for _, key := range []string{"trim", "router", "memory"} {
		err := s.UpdateWorkspaceSetting(ctx, org, ws, key, func(cur any) (string, error) {
			if cur != nil {
				t.Errorf("%s: a fresh workspace has no stored key, got %v", key, cur)
			}
			return `{"mode":"observe"}`, nil
		})
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	if _, err := s.UpdateWorkspaceMonitors(ctx, org, ws, func(c contract.MonitorConfig) (contract.MonitorConfig, error) { return c, nil }); err != nil {
		t.Fatal(err)
	}
	got, err := s.UpdateWorkspace(ctx, org, ws, nil, map[string]any{"theme": "x", "trim": "no", "router": "no", "memory": "no", "monitors": "no"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"trim", "router", "memory", "monitors"} {
		if m, ok := got.Settings[key].(map[string]any); !ok || (key != "monitors" && m["mode"] != "observe") {
			t.Errorf("%s after a settings replace = %v, want the stored value", key, got.Settings[key])
		}
	}
	if got.Settings["theme"] != "x" {
		t.Errorf("settings = %v, want theme kept", got.Settings)
	}
	if err := s.UpdateWorkspaceSetting(ctx, org, ws, "monitors", func(any) (string, error) { return `{}`, nil }); code(err) != contract.CodeValidationFailed {
		t.Errorf("monitors is not a generic key: %v", err)
	}
	if err := s.UpdateWorkspaceSetting(ctx, org, ws, "trim", func(any) (string, error) { return "", contract.Invalid("x", "bad") }); code(err) != contract.CodeValidationFailed {
		t.Errorf("an apply error must come back as is: %v", err)
	}
	if w, _ := s.Workspace(ctx, org, ws); w.Settings["theme"] != "x" || w.Settings["trim"] == nil {
		t.Errorf("a rejected update changed settings: %v", w.Settings)
	}
	if err := s.UpdateWorkspaceSetting(ctx, org, "nope", "trim", func(any) (string, error) { return `{}`, nil }); code(err) != contract.CodeNotFound {
		t.Errorf("malformed id: %v", err)
	}
	if err := s.UpdateWorkspaceSetting(ctx, "00000000-0000-0000-0000-000000000000", ws, "trim", func(any) (string, error) { return `{}`, nil }); code(err) != contract.CodeNotFound {
		t.Errorf("another org: %v", err)
	}
}
