package savings_test

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/nonet"
	"github.com/ethosagent/meterwise/internal/savings"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

var (
	t0 = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
)

func micro(v int64) *contract.MicroUSD { m := contract.MicroUSD(v); return &m }

func rows() []savings.PriceRow {
	return []savings.PriceRow{
		{Provider: "openai", Model: "gpt-4o", InputPerM: 2_500_000, OutputPerM: 10_000_000, CachedPerM: micro(1_250_000), EffectiveFrom: t0},
		{Provider: "openai", Model: "gpt-4o", InputPerM: 5_000_000, OutputPerM: 20_000_000, CachedPerM: micro(2_500_000), EffectiveFrom: t1},
		{Provider: "openai", Model: "gpt-4o-mini", InputPerM: 150_000, OutputPerM: 600_000, EffectiveFrom: t0},
		{Provider: "openai", Model: "o3", InputPerM: 2_000_000, OutputPerM: 8_000_000, ReasoningPerM: micro(9_000_000), EffectiveFrom: t0},
		{Provider: "anthropic", Model: "claude-sonnet-4", InputPerM: 3_000_000, OutputPerM: 15_000_000, CachedPerM: micro(300_000), EffectiveFrom: t0},
	}
}

func TestCost(t *testing.T) {
	p := savings.NewTablePricer(rows())
	mid := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name            string
		provider, model string
		u               contract.Usage
		at              time.Time
		want            contract.MicroUSD
		ok              bool
	}{
		{"openai input includes cached: uncached at input rate, cached at cached rate",
			"openai", "gpt-4o", contract.Usage{Input: 1000, Output: 500, Cached: 400}, mid,
			// 600*2.5 + 400*1.25 + 500*10 = 1500+500+5000 micro-dollars per thousand = 7000
			7000, true},
		{"the row effective at the call time wins", "openai", "gpt-4o", contract.Usage{Input: 1000, Output: 500, Cached: 400}, late, 14000, true},
		{"before the first row there is no price", "openai", "gpt-4o", contract.Usage{Input: 1}, t0.Add(-time.Hour), 0, false},
		{"dated model name takes the longest matching prefix", "openai", "gpt-4o-mini-2024-07-18", contract.Usage{Input: 1_000_000, Output: 1_000_000}, mid, 750_000, true},
		{"gpt-4o does not swallow gpt-4o-mini", "openai", "gpt-4o-mini", contract.Usage{Input: 1_000_000}, mid, 150_000, true},
		{"no cached rate: cached tokens bill at the input rate", "openai", "gpt-4o-mini", contract.Usage{Input: 1000, Cached: 1000}, mid, 150, true},
		{"anthropic input excludes cached: both bill", "anthropic", "claude-sonnet-4-20250514", contract.Usage{Input: 1000, Output: 100, Cached: 2000}, mid,
			// 1000*3 + 2000*0.3 + 100*15 = 3000+600+1500 = 5100
			5100, true},
		{"reasoning rate splits reasoning out of output", "openai", "o3", contract.Usage{Input: 0, Output: 1000, Reasoning: 400}, mid,
			// 600*8 + 400*9 = 4800+3600 = 8400 per thousand tokens
			8400, true},
		{"unknown model is not priced", "openai", "gpt-9", contract.Usage{Input: 10}, mid, 0, false},
		{"unknown provider is not priced", "mistral", "gpt-4o", contract.Usage{Input: 10}, mid, 0, false},
		{"a different model that shares a prefix stays unpriced", "anthropic", "claude-sonnet-4-5", contract.Usage{Input: 10}, mid, 0, false},
		{"-latest is the same model", "anthropic", "claude-sonnet-4-latest", contract.Usage{Input: 1_000_000}, mid, 3_000_000, true},
		{"a qualifier after a colon is the same model", "openai", "gpt-4o-mini:ft-acme", contract.Usage{Input: 1_000_000}, mid, 150_000, true},
		{"model name must match at a boundary", "openai", "gpt-4oo", contract.Usage{Input: 10}, mid, 0, false},
		{"rounds half up to a whole micro-dollar", "openai", "gpt-4o-mini", contract.Usage{Input: 3}, mid, 0, true}, // 0.45 -> 0
		{"cached larger than input does not go negative", "openai", "gpt-4o", contract.Usage{Input: 100, Cached: 500}, mid, 125, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := p.Cost(tc.provider, tc.model, tc.u, tc.at)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("Cost = (%d, %v), want (%d, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestCostOverflowIsUnknownNotWrapped(t *testing.T) {
	p := savings.NewTablePricer(rows())
	if got, ok := p.Cost("openai", "gpt-4o", contract.Usage{Input: 1 << 62}, t1); ok {
		t.Fatalf("an overflowing usage priced as %d; want not ok", got)
	}
	if _, ok := p.Cost("openai", "gpt-4o", contract.Usage{Input: -5}, t1); ok {
		t.Fatal("negative tokens must not price")
	}
}

func TestEmptyPricerKnowsNothing(t *testing.T) {
	var p savings.TablePricer
	if _, ok := p.Cost("openai", "gpt-4o", contract.Usage{Input: 1}, t1); ok {
		t.Fatal("zero-value pricer priced something")
	}
}

func TestReplaceSwapsTheSnapshotWhileReaders(t *testing.T) {
	p := savings.NewTablePricer(rows()[:1])
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 2000 {
				p.Cost("openai", "gpt-4o", contract.Usage{Input: 10}, t1)
			}
		}()
	}
	p.Replace(rows())
	wg.Wait()
	if _, ok := p.Cost("openai", "gpt-4o-mini", contract.Usage{Input: 10}, t1); !ok {
		t.Fatal("replacement not visible")
	}
	p.Replace(nil) // a failed or empty refresh is the loader's to skip; Replace itself obeys
	if _, ok := p.Cost("openai", "gpt-4o", contract.Usage{Input: 10}, t1); ok {
		t.Fatal("Replace(nil) should empty the snapshot")
	}
}
