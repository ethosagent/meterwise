package api_test

import (
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

// A call with no client family stores a NULL client. A biggest-saves row whose acted event sits on such a
// call must still list (client "" on the wire is null), not turn the whole page into a 503.
func TestSavesListsAnActedEventOnANullClientCall(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	c := callRec(ws, "n1", "sn", 1, time.Now().UTC().Add(-time.Hour), microp(3_000_000))
	c.Client = ""
	c.Blocked = true
	c.Verdicts = []contract.PricedVerdict{savingVerdict(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ModeBlock, contract.ConfidenceCertain, 100, 1_000_000, "src/a.go")}
	s.seedCalls(t, c)

	raw := s.must(t, admin.tok, "GET", "/api/workspaces/"+ws+"/savings/saves", "", 200)
	list := decodeAs[contract.SaveList](t, raw)
	if len(list.Items) != 1 || list.Items[0].CallID != "n1" || list.Items[0].Client != nil {
		t.Fatalf("saves = %s", raw)
	}
}
