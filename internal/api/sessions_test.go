package api_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

func microp(v int64) *contract.MicroUSD { m := contract.MicroUSD(v); return &m }

// seedCalls writes ledger rows the way internal/ledger does (through the store's one writer).
func (s *stack) seedCalls(t *testing.T, recs ...contract.CallRecord) {
	t.Helper()
	if _, err := s.db.WriteCalls(context.Background(), recs); err != nil {
		t.Fatal(err)
	}
}

func callRec(ws, id, sess string, idx int, at time.Time, cost *contract.MicroUSD) contract.CallRecord {
	return contract.CallRecord{
		ID: id, OrgID: store.OrgID(), WorkspaceID: ws, SessionID: sess, CallIndex: idx, StartedAt: at, Provider: "openai", Model: "gpt-4o",
		Wire: "openai.chat", Stream: true, Status: 200, Finish: "ok", Usage: contract.Usage{Input: 100, Output: 10, Cached: 5, Reasoning: 2},
		Cost: cost, TTFBMs: 12, DurationMs: 340, ToolCalls: []string{"read_file"}, SessionSource: contract.SessionSourceHeader,
		SessionStart: at, LastHadText: true, Client: "opencode/1.2",
	}
}

func TestSessionsListDetailAndStats(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	now := time.Now().UTC()
	s.seedCalls(t,
		callRec(ws, "a1", "sess-a", 1, now.Add(-3*time.Hour), microp(1_500_000)),
		callRec(ws, "a2", "sess-a", 2, now.Add(-2*time.Hour), nil),
		callRec(ws, "b1", "sess-b", 1, now.Add(-time.Hour), microp(250_000)),
		callRec(ws, "old", "sess-old", 1, now.AddDate(0, 0, -20), microp(2_000_000)),
	)
	if ok, err := s.db.CloseSession(context.Background(), ws, "sess-a", now); err != nil || !ok {
		t.Fatal(ok, err)
	}

	raw := s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/sessions?limit=2", "", 200)
	list := decodeAs[contract.SessionList](t, raw)
	if len(list.Items) != 2 || list.NextCursor == nil || list.Items[0].ID != "sess-b" || list.Items[1].ID != "sess-a" {
		t.Fatalf("page 1 = %s", raw)
	}
	a := list.Items[1]
	if a.Calls != 2 || a.InputTokens != 200 || a.CostUsd == nil || *a.CostUsd != 1_500_000 || a.Outcome != contract.OutcomeCompleted || a.ClosedAt == nil || a.Source != "header" ||
		a.Client == nil || *a.Client != "opencode/1.2" || a.WorkspaceID != ws || a.SavedUsd != 0 || a.LikelySavedUsd != 0 {
		t.Fatalf("session a = %+v", a)
	}
	if !strings.Contains(string(raw), `"costUsd":"1.500000"`) || !strings.Contains(string(raw), `"savedUsd":"0.000000"`) {
		t.Errorf("money must be decimal strings: %s", raw)
	}
	p2 := decodeAs[contract.SessionList](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/sessions?limit=2&cursor="+*list.NextCursor, "", 200))
	if len(p2.Items) != 1 || p2.Items[0].ID != "sess-old" || p2.NextCursor != nil {
		t.Fatalf("page 2 = %+v", p2)
	}
	from := now.Add(-150 * time.Minute).Format(time.RFC3339)
	rng := decodeAs[contract.SessionList](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/sessions?from="+from, "", 200))
	if len(rng.Items) != 1 || rng.Items[0].ID != "sess-b" {
		t.Fatalf("from filter = %+v", rng)
	}

	d := decodeAs[contract.SessionDetail](t, s.must(t, admin.tok, "GET", "/api/sessions/sess-a?workspaceId="+ws, "", 200))
	if d.Session.ID != "sess-a" || len(d.Calls) != 2 || d.Calls[0].ID != "a1" || d.Calls[1].CostUsd != nil || d.Calls[0].CostUsd == nil {
		t.Fatalf("detail = %+v", d)
	}
	c := d.Calls[0]
	if c.CallIndex != 1 || c.Provider != "openai" || c.Model != "gpt-4o" || c.Wire != "openai.chat" || !c.Stream || c.Status != 200 || c.Finish != "ok" ||
		c.InputTokens != 100 || c.OutputTokens != 10 || c.CachedTokens != 5 || c.ReasoningTokens != 2 || c.TtfbMs != 12 || c.DurationMs != 340 ||
		len(c.ToolCalls) != 1 || c.ToolCalls[0] != "read_file" || c.Blocked || c.NotesInjected != 0 {
		t.Fatalf("call = %+v", c)
	}

	st := decodeAs[contract.StatsView](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/stats", "", 200))
	if st.Calls != 3 || st.Sessions != 2 || st.SpendUsd != 1_750_000 || st.AvgCallsPerSession != 1.5 || len(st.ByDay) != 7 {
		t.Fatalf("stats 7d = %+v", st)
	}
	if last := st.ByDay[6]; last.Day != now.Format(time.DateOnly) && last.Day != now.Add(-3*time.Hour).Format(time.DateOnly) {
		t.Errorf("by day is not oldest first ending today: %+v", st.ByDay)
	}
	st30 := decodeAs[contract.StatsView](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/stats?range=30d", "", 200))
	if st30.Calls != 4 || st30.SpendUsd != 3_750_000 || len(st30.ByDay) != 30 {
		t.Fatalf("stats 30d = %+v", st30)
	}
	empty := decodeAs[contract.StatsView](t, s.must(t, admin.tok, "GET", "/api/workspaces/"+s.newWorkspace(t, admin.tok, "Empty")+"/stats", "", 200))
	if empty.Calls != 0 || empty.AvgCallsPerSession != 0 || len(empty.ByDay) != 7 || empty.ByDay[0].Calls != 0 {
		t.Fatalf("empty stats = %+v", empty)
	}
}

func TestSessionValidationAndScoping(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	other := s.newWorkspace(t, admin.tok, "Other")
	now := time.Now().UTC()
	s.seedCalls(t, callRec(ws, "x1", "shared", 1, now, nil))
	s.seedCalls(t, callRec(other, "y1", "shared", 1, now, nil))

	for _, q := range []string{"limit=0", "limit=abc", "limit=201", "from=yesterday", "to=nope", "cursor=not-a-cursor", "from=2030-01-02T00:00:00Z&to=2030-01-01T00:00:00Z"} {
		code, b := s.call(t, "GET", "/api/workspaces/"+ws+"/sessions?"+q, admin.tok, "")
		wantErr(t, code, b, 422, contract.CodeValidationFailed)
	}
	code, b := s.call(t, "GET", "/api/workspaces/"+ws+"/stats?range=1y", admin.tok, "")
	wantErr(t, code, b, 422, contract.CodeValidationFailed)

	// workspaceId is required and authorizes before any read
	code, b = s.call(t, "GET", "/api/sessions/shared", admin.tok, "")
	wantErr(t, code, b, 422, contract.CodeValidationFailed)
	code, b = s.call(t, "GET", "/api/sessions/shared?workspaceId=00000000-0000-0000-0000-00000000dead", admin.tok, "")
	wantErr(t, code, b, 404, contract.CodeNotFound)
	// the same session id lives separately in each workspace; a mismatch is 404, never the other's data
	d := decodeAs[contract.SessionDetail](t, s.must(t, admin.tok, "GET", "/api/sessions/shared?workspaceId="+other, "", 200))
	if d.Session.WorkspaceID != other || len(d.Calls) != 1 || d.Calls[0].ID != "y1" {
		t.Fatalf("detail = %+v", d)
	}
	code, b = s.call(t, "GET", "/api/sessions/never-existed?workspaceId="+ws, admin.tok, "")
	wantErr(t, code, b, 404, contract.CodeNotFound)
	code, b = s.call(t, "GET", "/api/workspaces/"+ws+"/sessions", "", "")
	if code != 401 {
		t.Fatalf("no token: %d %s", code, b)
	}
}

func TestSessionAndStatsPermissions(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	_, ws := s.defaultWorkspace(t, admin.tok)
	s.seedCalls(t, callRec(ws, "z1", "sz", 1, time.Now().UTC(), microp(10)))
	routes := []string{"/api/workspaces/" + ws + "/sessions", "/api/sessions/sz?workspaceId=" + ws, "/api/workspaces/" + ws + "/stats"}

	put := func(grants ...string) {
		s.must(t, admin.tok, "PUT", "/api/organization/permissions", `{"grants":[`+strings.Join(grants, ",")+`]}`, 200)
	}
	grant := func(rt, act string) string {
		return `{"resourceType":"` + rt + `","action":"` + act + `","workspaceId":null}`
	}
	put(grant("workspace", "read"))
	for _, r := range routes {
		code, b := s.call(t, "GET", r, m.tok, "")
		if code != 403 {
			t.Fatalf("%s without grants: %d %s", r, code, b)
		}
	}
	put(grant("workspace", "read"), grant("session", "read"))
	for i, r := range routes {
		code, b := s.call(t, "GET", r, m.tok, "")
		wantCode := 200
		if i == 2 { // stats needs savings:read, not session:read
			wantCode = 403
		}
		if code != wantCode {
			t.Fatalf("%s with session:read: %d %s, want %d", r, code, b, wantCode)
		}
	}
	put(grant("workspace", "read"), grant("savings", "read"))
	code, b := s.call(t, "GET", routes[2], m.tok, "")
	if code != 200 {
		t.Fatalf("stats with savings:read: %d %s", code, b)
	}
	code, b = s.call(t, "GET", routes[1], m.tok, "")
	if code != 403 {
		t.Fatalf("session detail with only savings:read: %d %s", code, b)
	}
}

func TestLedgerRoutesAnswerNotAvailableWhenTheLedgerIsNotWired(t *testing.T) {
	f := newFixture(t, func(d *api.Deps) { d.Ledger = nil })
	for _, path := range []string{"/api/workspaces/w/sessions", "/api/sessions/s?workspaceId=w", "/api/workspaces/w/stats"} {
		resp, b := f.do(t, "GET", path, "")
		wantErr(t, resp.StatusCode, b, 501, contract.CodeNotAvailable)
	}
}
