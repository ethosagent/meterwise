package ledger

import (
	"context"
	"log/slog"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/savings"
)

// PriceSource lists the pricing table; *store.Store implements it.
type PriceSource interface {
	Pricing(ctx context.Context) ([]contract.PriceRow, error)
}

// PricingRefresh is how often the loader re-reads the pricing table.
const PricingRefresh = 5 * time.Minute

// PriceLoader keeps a savings.TablePricer's snapshot in step with the pricing table. savings stays
// pure (ruling R11): the I/O lives here.
type PriceLoader struct {
	Source PriceSource
	Pricer *savings.TablePricer
	Every  time.Duration // default PricingRefresh
	Log    *slog.Logger
}

// Load reads the table once and swaps the snapshot. An error or an empty table keeps the previous
// snapshot (a failed refresh must not make every call unpriced).
func (l *PriceLoader) Load(ctx context.Context) error {
	rows, err := l.Source.Pricing(ctx)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return errEmptyPricing
	}
	l.Pricer.Replace(rows)
	return nil
}

type pricingError string

func (e pricingError) Error() string { return string(e) }

const errEmptyPricing = pricingError("ledger: the pricing table is empty; keeping the previous prices")

// Run reloads every Every until ctx ends, starting one tick from now: the caller has just loaded the table
// (startLedger does, synchronously), so a second immediate read would be wasted. Failures are logged and
// the next tick retries.
func (l *PriceLoader) Run(ctx context.Context) {
	every := l.Every
	if every <= 0 {
		every = PricingRefresh
	}
	log := l.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	load := func() {
		if err := l.Load(ctx); err != nil && ctx.Err() == nil {
			log.LogAttrs(ctx, slog.LevelError, "ledger: pricing refresh failed", slog.String("err", err.Error()))
		}
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			load()
		}
	}
}
