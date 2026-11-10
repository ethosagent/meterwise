package dataplane_test

import (
	"reflect"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/dataplane"
)

type namedHook struct {
	tollgate.NoopHook
	name string
}

func (h namedHook) Name() string { return h.name }

func names(hs []tollgate.Hook) []string {
	var out []string
	for _, h := range hs {
		out = append(out, h.Name())
	}
	return out
}

func TestHookChainOrderIsWsauthHistoryMonitorsInjectedLedger(t *testing.T) {
	hooks, sources := dataplane.HookChain(dataplane.ChainOptions{PrivacyMode: true}, []tollgate.Hook{namedHook{name: "extra"}}, namedHook{name: "ledger"})
	want := []string{"wsauth", "history", "monitors", "extra", "ledger"}
	if got := names(hooks); !reflect.DeepEqual(got, want) {
		t.Fatalf("chain = %v, want %v", got, want)
	}
	if len(sources) != 2 {
		t.Fatalf("status sources = %d, want history and monitors", len(sources))
	}
}

func TestHookChainWithoutInjectedHooks(t *testing.T) {
	hooks, _ := dataplane.HookChain(dataplane.ChainOptions{}, nil, namedHook{name: "ledger"})
	if got, want := names(hooks), []string{"wsauth", "history", "monitors", "ledger"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("chain = %v, want %v", got, want)
	}
}
