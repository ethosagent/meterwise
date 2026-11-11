package ledger_test

import (
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// A routed call the upstream answered with an error (404 on the cheaper model, say) has no usage, but the router's decision is still
// recorded, with a saving of 0, so the router stats can count it as unavailable. Trim and the hint are not recorded:
// nothing reached the model.
func TestARefusedRoutedCallRecordsItsRouteAndNothingElse(t *testing.T) {
	for _, status := range []int{400, 403, 404, 429, 500} {
		c := directCall("c1", "hi", nil, 1)
		c.Model = "gpt-4o-mini"
		c.Meta[contract.MetaRouter] = contract.RouteDecision{Mode: contract.RouterModeConservative, Action: contract.RouteRouted, From: "gpt-4o", To: "gpt-4o-mini", ScoreBp: 2500}
		c.Meta[contract.MetaTrim] = trimMeta(contract.TrimActed, 4000, 3000, stage(contract.StageDupLines, 900, 300, false))
		c.Meta[contract.MetaMemory] = contract.MemoryHint{Injected: true}
		rec, _ := runSavers(t, c, func(m *tollgate.Metric) {
			m.Status, m.Finish, m.Model, m.Usage = status, tollgate.FinishUpstreamError, "gpt-4o-mini", tollgate.Usage{}
		})
		if rec.RouterAction == nil || *rec.RouterAction != contract.RouteRouted || rec.RoutedFrom != "gpt-4o" || rec.RoutedTo != "gpt-4o-mini" || rec.SavedUSDRouter != 0 {
			t.Errorf("status %d: route = %+v saved %d", status, rec.RouterAction, rec.SavedUSDRouter)
		}
		if rec.TrimAction != nil || rec.HintInjected || len(rec.TrimEvents) != 0 {
			t.Errorf("status %d: a refused call recorded trim or a hint: %+v", status, rec)
		}
	}
	// a 2xx with no usage never reached a model: nothing is recorded, as before
	for _, status := range []int{200, 204} {
		c := directCall("c2", "hi", nil, 1)
		c.Meta[contract.MetaRouter] = contract.RouteDecision{Action: contract.RouteRouted, From: "gpt-4o", To: "gpt-4o-mini"}
		if rec, _ := runSavers(t, c, func(m *tollgate.Metric) { m.Status, m.Usage = status, tollgate.Usage{} }); rec.RouterAction != nil {
			t.Errorf("status %d with no usage recorded a route", status)
		}
	}
}
