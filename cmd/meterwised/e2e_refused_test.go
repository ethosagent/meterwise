package main

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"
	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/store"
)

// A call the provider policy refuses (403) never reached the upstream: it is not a calls row or a
// session, it does not move last_call_at or the stats, and /statusz counts it by status.
func TestARefusedCallIsNotARowAndDoesNotTouchLastCallAt(t *testing.T) {
	up := testkit.NewFakeUpstream(t, testkit.Options{})
	base, _ := url.Parse(up.URL)
	res := dataplane.NewWorkspaceResolver(map[string]tollgate.Provider{
		"openai":    {Name: "openai", Wire: tollgate.WireOpenAIChat, BaseURL: base},
		"anthropic": {Name: "anthropic", Wire: tollgate.WireAnthropicMessages, BaseURL: base},
	}, false)
	p := bootPlane(t, dataplane.Deps{Resolver: res}, nil)
	pr := contract.Principal{OrgID: store.OrgID(), Role: contract.RoleAdmin}
	if _, err := p.rt.svc.UpdateWorkspace(context.Background(), pr, p.ws.ID, contract.UpdateWorkspaceRequest{
		Settings: map[string]any{"allowedProviders": []any{"anthropic"}},
	}); err != nil {
		t.Fatal(err)
	}
	if code, _, body := p.call(t, p.ws.PublicID, "openai/chat", "refused-session"); code != 403 {
		t.Fatalf("a disallowed provider: %d %s", code, body)
	}
	time.Sleep(700 * time.Millisecond) // well past the batcher's flush interval
	var setup contract.Setup
	p.getJSON(t, "/api/workspaces/"+p.ws.ID+"/setup", &setup)
	if setup.Status.FirstCallAt != nil || setup.Status.LastCallAt != nil || setup.Status.CallsToday != 0 {
		t.Fatalf("a refused call moved the setup status: %+v", setup.Status)
	}
	if code, _, body := p.call(t, p.ws.PublicID, "anthropic/messages", "ok-session"); code != 200 {
		t.Fatalf("an allowed provider: %d %s", code, body)
	}
	p.waitCalls(t, 1)
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	p.getJSON(t, "/api/workspaces/"+p.ws.ID+"/sessions", &list)
	if len(list.Items) != 1 || list.Items[0].ID != "ok-session" {
		t.Fatalf("sessions = %+v, want only ok-session", list.Items)
	}
	code, body := get(t, p.admin+"/statusz")
	var doc struct {
		Sources map[string]map[string]any `json:"sources"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &doc) != nil {
		t.Fatalf("statusz %d %s", code, body)
	}
	refused, _ := doc.Sources["ledger"]["refused_by_status"].(map[string]any)
	if refused["403"] != float64(1) || doc.Sources["ledger"]["flushed"] != float64(1) {
		t.Fatalf("ledger status %v", doc.Sources["ledger"])
	}
}
