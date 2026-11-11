package dataplane

import (
	"reflect"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

type slotHook struct {
	tollgate.NoopHook
	name string
}

func (h slotHook) Name() string { return h.name }

type slotSource struct{ slotHook }

func (slotSource) Status() map[string]any { return nil }

func hookNames(hs []tollgate.Hook) []string {
	var out []string
	for _, h := range hs {
		out = append(out, h.Name())
	}
	return out
}

// Every slot filled: the one order of ADR 0026, with the injected hooks before the ledger.
func TestOrderPutsEverySlotWhereTheADRSaysAndSkipsNilOnes(t *testing.T) {
	s := chainSlots{
		wsauth: slotHook{name: "wsauth"}, history: slotHook{name: "history"}, trim: slotSource{slotHook{name: "trim"}},
		memory: slotHook{name: "memory"}, router: slotSource{slotHook{name: "router"}}, monitors: slotHook{name: "monitors"},
	}
	hooks, srcs := s.order([]tollgate.Hook{slotHook{name: "extra"}}, slotHook{name: "ledger"})
	want := []string{"wsauth", "history", "trim", "memory", "router", "monitors", "extra", "ledger"}
	if got := hookNames(hooks); !reflect.DeepEqual(got, want) {
		t.Fatalf("chain = %v, want %v", got, want)
	}
	if len(srcs) != 2 {
		t.Errorf("only the slots that report status are sources: %d", len(srcs))
	}

	s.trim, s.memory = nil, nil
	hooks, _ = s.order(nil, slotHook{name: "ledger"})
	if got, want := hookNames(hooks), []string{"wsauth", "history", "router", "monitors", "ledger"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nil slots must vanish: %v, want %v", got, want)
	}
}

// The seam the router owner replaces returns nil today (trim's is filled: feature_trim_test.go), so the default chain has no router.
func TestZeroChainOptionsBuildNoRouterOrMemoryHook(t *testing.T) {
	if routerHook(ChainOptions{}) != nil {
		t.Fatal("zero ChainOptions must build no router hook")
	}
	if (ChainOptions{}).Router.Routes != nil || (ChainOptions{}).Memory != nil {
		t.Fatal("zero ChainOptions must carry no router tables and no memory hook")
	}
	_ = contract.RouterTables{}
}
