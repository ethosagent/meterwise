package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// TestTrimRoutesOverTheRealReader drives GET /trim/stats and GET /calls/{id}/trim through the gateway with the
// store-backed port: 200 with the numbers the ledger wrote, 403 without the permission, 404 for another workspace's
// call, 422 for a bad range. The preview carries sizes and fixed markers only.
func TestTrimRoutesOverTheRealReader(t *testing.T) {
	s := newStackWith(t, func(d *api.Deps) { d.Trim = store.NewTrimReader(d.DB.(*store.Store)) })
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	base := "/api/workspaces/" + ws
	now := time.Now().UTC().Add(-time.Hour)
	act, conf := contract.TrimActed, contract.ConfidenceCertain
	call := contract.CallRecord{
		ID: "tc1", OrgID: store.OrgID(), WorkspaceID: ws, SessionID: "ts1", CallIndex: 1, StartedAt: now, Provider: "openai", Model: "gpt-4o",
		Wire: "openai.chat", Stream: true, Status: 200, Finish: "ok", Usage: contract.Usage{Input: 100, Output: 10}, SessionSource: contract.SessionSourceHeader,
		SessionStart: now, TrimAction: &act, TrimConfidence: &conf, TrimTokens: 500, SavedUSDTrim: 750_000,
		TrimEvents: []contract.TrimEventRecord{{OrgID: store.OrgID(), WorkspaceID: ws, SessionID: "ts1", CallID: "tc1", TS: now,
			Stage: contract.StageLogWindow, Tool: "get_build_log", Regions: 1, BytesBefore: 2000, BytesAfter: 500, EstTokens: 500, Action: act}},
	}
	if _, err := s.db.WriteCalls(context.Background(), []contract.CallRecord{call}); err != nil {
		t.Fatal(err)
	}
	st := decodeAs[contract.TrimStats](t, s.must(t, admin.tok, "GET", base+"/trim/stats?range=7d", "", 200))
	if st.Calls != 1 || st.TrimmedCalls != 1 || st.SavedUsd != 750_000 || st.TrimTokens != 500 || len(st.ByStage) != 5 || st.Mode != contract.TrimModeObserve {
		t.Errorf("stats = %+v", st)
	}
	if len(st.ByTool) != 1 || st.ByTool[0].Tool != "get_build_log" {
		t.Errorf("by tool = %+v", st.ByTool)
	}
	p := decodeAs[contract.TrimPreview](t, s.must(t, admin.tok, "GET", base+"/calls/tc1/trim", "", 200))
	if p.CallID != "tc1" || p.Action == nil || p.BodyBefore != 2000 || p.BodyAfter != 500 || len(p.Stages) != 1 || p.Stages[0].Marker == "" {
		t.Errorf("preview = %+v", p)
	}
	code, b := s.call(t, "GET", base+"/trim/stats?range=1y", admin.tok, "")
	wantErr(t, code, b, 422, contract.CodeValidationFailed)
	code, b = s.call(t, "GET", base+"/calls/missing/trim", admin.tok, "")
	wantErr(t, code, b, 404, contract.CodeNotFound)

	outsider := s.invited(t, admin.tok, "user_member", contract.RoleMember)
	s.must(t, outsider.tok, "GET", base+"/trim/stats", "", 200) // savings:read is a member default
	code, b = s.call(t, "GET", base+"/trim/stats", "", "")
	wantErr(t, code, b, 401, contract.CodeUnauthenticated)
	code, b = s.call(t, "PATCH", base+"/trim", outsider.tok, `{"mode":"lossless"}`)
	wantErr(t, code, b, 403, contract.CodeForbidden)
	code, b = s.call(t, "GET", "/api/workspaces/"+missingWS+"/trim/stats", admin.tok, "")
	wantErr(t, code, b, 404, contract.CodeNotFound)
}
