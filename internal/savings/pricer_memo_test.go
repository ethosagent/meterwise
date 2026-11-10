package savings_test

import (
	"fmt"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/savings"
)

// The model lookup is memoised per snapshot (carry L17): a repeated lookup, a miss and a swapped snapshot
// all price exactly as an uncached scan would, and a flood of distinct unknown names stays correct.
func TestCostMemoIsTransparent(t *testing.T) {
	row := func(model string, in int64) savings.PriceRow {
		return savings.PriceRow{Provider: "openai", Model: model, InputPerM: contract.MicroUSD(in), OutputPerM: 1, EffectiveFrom: t0}
	}
	p := savings.NewTablePricer([]savings.PriceRow{row("gpt-4o", 2_000_000), row("gpt-4o-mini", 100_000)})
	u := contract.Usage{Input: 1_000_000}
	for range 3 { // first lookup fills the memo, later ones read it
		if got, ok := p.Cost("openai", "gpt-4o-2024-08-06", u, t1); !ok || got != 2_000_000 {
			t.Fatalf("dated gpt-4o = %v %v", got, ok)
		}
		if got, ok := p.Cost("openai", "gpt-4o-mini", u, t1); !ok || got != 100_000 {
			t.Fatalf("gpt-4o-mini = %v %v", got, ok)
		}
		if _, ok := p.Cost("openai", "gpt-9", u, t1); ok {
			t.Fatal("an unknown model must stay unpriced")
		}
	}
	p.Replace([]savings.PriceRow{row("gpt-9", 5_000_000)}) // a new snapshot starts with an empty memo
	if got, ok := p.Cost("openai", "gpt-9", u, t1); !ok || got != 5_000_000 {
		t.Fatalf("after Replace gpt-9 = %v %v", got, ok)
	}
	if _, ok := p.Cost("openai", "gpt-4o", u, t1); ok {
		t.Fatal("a replaced model must not be served from the old memo")
	}
	for i := range 5000 { // beyond the memo bound lookups still answer correctly
		if _, ok := p.Cost("openai", fmt.Sprintf("unknown-%d", i), u, t1); ok {
			t.Fatal("unknown model priced")
		}
	}
	if got, ok := p.Cost("openai", "gpt-9", u, t1); !ok || got != 5_000_000 {
		t.Fatalf("after the flood gpt-9 = %v %v", got, ok)
	}
}
