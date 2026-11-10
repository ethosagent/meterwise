package ledger_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/ledger"
	"github.com/ethosagent/meterwise/internal/savings"
)

type fakeSource struct {
	mu   sync.Mutex
	rows []contract.PriceRow
	err  error
	n    int
}

func (f *fakeSource) Pricing(context.Context) ([]contract.PriceRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	return f.rows, f.err
}

func (f *fakeSource) set(rows []contract.PriceRow, err error) {
	f.mu.Lock()
	f.rows, f.err = rows, err
	f.mu.Unlock()
}

func TestPriceLoaderRefreshesAndKeepsTheSnapshotOnFailure(t *testing.T) {
	row := func(model string, in int64) contract.PriceRow {
		return contract.PriceRow{Provider: "openai", Model: model, InputPerM: contract.MicroUSD(in), OutputPerM: 1, EffectiveFrom: epoch}
	}
	src := &fakeSource{rows: []contract.PriceRow{row("gpt-4o", 2_000_000)}}
	p := savings.NewTablePricer(nil)
	l := &ledger.PriceLoader{Source: src, Pricer: p}
	if err := l.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	cost := func(model string) bool {
		_, ok := p.Cost("openai", model, contract.Usage{Input: 1}, time.Now())
		return ok
	}
	if !cost("gpt-4o") {
		t.Fatal("loaded price not visible")
	}
	src.set(nil, errors.New("db down"))
	if err := l.Load(context.Background()); err == nil || !cost("gpt-4o") {
		t.Fatalf("a failed refresh must keep the old prices (err %v)", err)
	}
	src.set(nil, nil)
	if err := l.Load(context.Background()); err == nil || !cost("gpt-4o") {
		t.Fatalf("an empty table must keep the old prices (err %v)", err)
	}
	src.set([]contract.PriceRow{row("gpt-5", 1_000_000)}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	l.Every = 5 * time.Millisecond
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for !cost("gpt-5") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if !cost("gpt-5") || cost("gpt-4o") {
		t.Fatal("the loader did not swap in the refreshed table")
	}
}

// Run does not read the table before its first tick: the caller loaded it synchronously just before
// (startLedger), so an immediate second read would only double the boot query.
func TestPriceLoaderRunWaitsOneTickBeforeTheFirstReload(t *testing.T) {
	src := &fakeSource{rows: []contract.PriceRow{{Provider: "openai", Model: "gpt-4o", InputPerM: 1, OutputPerM: 1, EffectiveFrom: epoch}}}
	l := &ledger.PriceLoader{Source: src, Pricer: savings.NewTablePricer(nil), Every: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	src.mu.Lock()
	defer src.mu.Unlock()
	if src.n != 0 {
		t.Fatalf("Run read the pricing table %d times before its first tick", src.n)
	}
}
