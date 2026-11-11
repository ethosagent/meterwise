package api_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

var routerDefaults = []contract.RoutePair{{Provider: "openai", From: "gpt-4o", To: "gpt-4o-mini"}, {Provider: "openai", From: "gpt-4.1", To: "gpt-4.1-mini"}}

func newRouterStack(t *testing.T) *stack {
	t.Helper()
	return newStackWith(t, func(d *api.Deps) { d.Router = store.NewRouterReader(d.DB.(*store.Store), routerDefaults) })
}

// routedCall is a routed call of session sess; fire adds a monitor verdict to it.
func routedCall(ws, id, sess string, at time.Time, saved contract.MicroUSD, fire bool) contract.CallRecord {
	r := callRec(ws, id, sess, 3, at, microp(100))
	action := contract.RouteRouted
	score := 2500
	r.RouterAction, r.RoutedFrom, r.RoutedTo, r.RouterScoreBp, r.SavedUSDRouter, r.Model = &action, "gpt-4o", "gpt-4o-mini", &score, saved, "gpt-4o-mini"
	if fire {
		r.Verdicts = []contract.PricedVerdict{{MonitorVerdict: contract.MonitorVerdict{Monitor: contract.MonitorDuplicateToolCall, Mode: contract.ModeObserve, Action: contract.ModeObserve}, Confidence: contract.ConfidenceLikely}}
	}
	return r
}

func TestRouterRoutesAgainstTheRealStore(t *testing.T) {
	s := newRouterStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	other := s.newWorkspace(t, admin.tok, "Other")
	base := "/api/workspaces/" + ws
	now := time.Now().UTC().Add(-time.Hour)
	s.seedCalls(t,
		routedCall(ws, "r1", "sess-r", now, 3_000_000, false),
		routedCall(other, "x1", "sess-x", now, 9_000_000, false),
		callRec(ws, "p1", "sess-p", 1, now, microp(100)),
	)

	// 200: settings carry the defaults and what the workspace used; stats reconcile with the rows
	st := decodeAs[contract.RouterSettings](t, s.must(t, admin.tok, "GET", base+"/router", "", 200))
	if len(st.Defaults) != 2 || len(st.Effective) != 2 || len(st.Suggestions) != 1 || st.Suggestions[0].From != "gpt-4o" {
		t.Errorf("settings = %+v", st)
	}
	stats := decodeAs[contract.RouterStats](t, s.must(t, admin.tok, "GET", base+"/router/stats?range=7d", "", 200))
	if stats.RoutedCalls != 1 || stats.Calls != 2 || stats.SavedUsd != 3_000_000 || stats.Mode != contract.RouterModeOff || len(stats.ByPair) != 1 {
		t.Errorf("stats = %+v (another workspace's routed call must not count)", stats)
	}

	// 200: feedback sets and clears; the audit row names the verdict
	fb := base + "/calls/r1/router-feedback"
	if v := decodeAs[contract.RouterFeedbackView](t, s.must(t, admin.tok, "POST", fb, `{"verdict":"worse"}`, 200)); v.Verdict == nil || *v.Verdict != "worse" {
		t.Errorf("feedback = %+v", v)
	}
	if got := decodeAs[contract.RouterStats](t, s.must(t, admin.tok, "GET", base+"/router/stats?range=7d", "", 200)).WorseFeedback; got != 1 {
		t.Errorf("worseFeedback = %d", got)
	}
	if n := s.count(t, `SELECT count(*) FROM audit_log WHERE action = 'router.feedback' AND resource_id = 'r1' AND workspace_id = $1::uuid AND metadata->>'verdict' = 'worse'`, ws); n != 1 {
		t.Errorf("router.feedback audit rows with verdict worse = %d, want 1", n)
	}
	s.must(t, admin.tok, "POST", fb, `{"verdict":null}`, 200)

	// 404: a call of another workspace, or none at all
	for _, id := range []string{"x1", "nope"} {
		code, b := s.call(t, "POST", base+"/calls/"+id+"/router-feedback", admin.tok, `{"verdict":"worse"}`)
		wantErr(t, code, b, 404, contract.CodeNotFound)
	}
	// 422: a bad verdict and a bad range
	code, b := s.call(t, "POST", fb, admin.tok, `{"verdict":"meh"}`)
	wantErr(t, code, b, 422, contract.CodeValidationFailed)
	code, b = s.call(t, "GET", base+"/router/stats?range=1y", admin.tok, "")
	wantErr(t, code, b, 422, contract.CodeValidationFailed)
	if n := s.count(t, `SELECT count(*) FROM router_feedback`); n != 0 {
		t.Errorf("refused or cleared feedback left %d rows", n)
	}
	// 403: a member with no grant on a workspace cannot read its router stats or settings or give feedback there
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	s.must(t, admin.tok, "PUT", "/api/organization/permissions", grantsJSON(), 200) // no org-wide grants for members
	ob := "/api/workspaces/" + other
	for _, c := range [][3]string{{"GET", ob + "/router/stats", ""}, {"POST", ob + "/calls/x1/router-feedback", `{"verdict":"worse"}`}, {"GET", ob + "/router", ""}, {"PATCH", ob + "/router", `{"mode":"observe"}`}} {
		code, b := s.call(t, c[0], c[1], m.tok, c[2])
		wantErr(t, code, b, 403, contract.CodeForbidden)
	}
}

// The payload's guardrail.warn is true exactly when the cheap fire rate is more than 200 bp above the strong one.
func TestRouterStatsGuardrailWarnOverTheAPI(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		cheapFired, strongFired int
		warn                    bool
	}{{"within 200 bp", 7, 6, false}, {"400 bp above", 8, 6, true}} {
		t.Run(tc.name, func(t *testing.T) {
			s := newRouterStack(t)
			admin := s.admin(t, "user_admin")
			_, ws := s.defaultWorkspace(t, admin.tok)
			at := time.Now().UTC().Add(-time.Hour)
			var recs []contract.CallRecord
			for i := range 50 {
				recs = append(recs, routedCall(ws, fmt.Sprintf("c%d", i), "sc", at, 1, i < tc.cheapFired))
				p := callRec(ws, fmt.Sprintf("p%d", i), "sp", 1, at, microp(100))
				if i < tc.strongFired {
					p.Verdicts = recs[i].Verdicts
				}
				recs = append(recs, p)
			}
			s.seedCalls(t, recs...)
			raw := s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/router/stats?range=7d", "", 200)
			g := decodeAs[contract.RouterStats](t, raw).Guardrail
			if g.Warn != tc.warn || g.CheapFireBp == nil || g.StrongFireBp == nil {
				t.Errorf("guardrail = %+v", g)
			}
			if !strings.Contains(string(raw), fmt.Sprintf(`"warn":%t`, tc.warn)) {
				t.Errorf("payload: %s", raw)
			}
		})
	}
}
