package savings

import (
	"math"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

const perMillion = 1_000_000

// PriceRow is one row of the pricing table: micro-USD per million tokens, effective from a time.
// CachedPerM and ReasoningPerM are nil when the provider has no separate rate: cached tokens then
// bill at the input rate and reasoning tokens at the output rate.
type PriceRow struct {
	Provider      string
	Model         string
	InputPerM     contract.MicroUSD
	OutputPerM    contract.MicroUSD
	CachedPerM    *contract.MicroUSD
	ReasoningPerM *contract.MicroUSD
	EffectiveFrom time.Time
}

// TablePricer prices usage from an immutable snapshot of the pricing table that Replace swaps
// atomically, so the hot path never takes a lock. The zero value knows no model.
type TablePricer struct{ snap atomic.Pointer[snapshot] }

type snapshot struct {
	byProvider map[string][]series // series sorted by model, then rows by effective_from
}

type series struct {
	model string
	rows  []PriceRow // ascending EffectiveFrom
}

var _ contract.Pricer = (*TablePricer)(nil)

// NewTablePricer builds a pricer over rows.
func NewTablePricer(rows []PriceRow) *TablePricer {
	p := &TablePricer{}
	p.Replace(rows)
	return p
}

// Replace swaps in a snapshot built from rows. rows is copied.
func (p *TablePricer) Replace(rows []PriceRow) {
	s := &snapshot{byProvider: map[string][]series{}}
	sorted := slices.Clone(rows)
	slices.SortStableFunc(sorted, func(a, b PriceRow) int {
		if c := strings.Compare(a.Provider, b.Provider); c != 0 {
			return c
		}
		if c := strings.Compare(a.Model, b.Model); c != 0 {
			return c
		}
		return a.EffectiveFrom.Compare(b.EffectiveFrom)
	})
	for _, r := range sorted {
		list := s.byProvider[r.Provider]
		if n := len(list); n > 0 && list[n-1].model == r.Model {
			list[n-1].rows = append(list[n-1].rows, r)
		} else {
			list = append(list, series{model: r.Model, rows: []PriceRow{r}})
		}
		s.byProvider[r.Provider] = list
	}
	p.snap.Store(s)
}

// Cost prices u for provider and model at time at. The model matches exactly or as the longest
// pricing-table name followed by a boundary ('-', ':' or '@'), so a dated name such as
// claude-sonnet-4-20250514 finds claude-sonnet-4 while gpt-4o never swallows gpt-4o-mini. Among
// the rows of that model the latest one effective at "at" wins. Token semantics follow the wire:
// OpenAI input includes cached tokens, Anthropic input excludes them. The result rounds half up
// to a whole micro-dollar. ok is false for an unknown model, a time before the first row, negative
// tokens, or an amount that would overflow int64: the call's cost is then NULL, never 0.
func (p *TablePricer) Cost(provider, model string, u contract.Usage, at time.Time) (contract.MicroUSD, bool) {
	s := p.snap.Load()
	if s == nil || u.Input < 0 || u.Output < 0 || u.Cached < 0 || u.Reasoning < 0 {
		return 0, false
	}
	row, ok := s.find(provider, model, at)
	if !ok {
		return 0, false
	}
	input, cached := u.Input, u.Cached
	if provider != "anthropic" { // OpenAI-style: cached tokens are part of the input count
		cached = min(cached, input)
		input -= cached
	}
	output, reasoning := u.Output, int64(0)
	if row.ReasoningPerM != nil {
		reasoning = min(u.Reasoning, output)
		output -= reasoning
	}
	cachedRate, reasoningRate := row.InputPerM, row.OutputPerM
	if row.CachedPerM != nil {
		cachedRate = *row.CachedPerM
	}
	if row.ReasoningPerM != nil {
		reasoningRate = *row.ReasoningPerM
	}
	var total int64
	for _, term := range [...]struct{ tokens, rate int64 }{
		{input, int64(row.InputPerM)}, {cached, int64(cachedRate)}, {output, int64(row.OutputPerM)}, {reasoning, int64(reasoningRate)},
	} {
		if term.tokens != 0 && term.rate > math.MaxInt64/term.tokens {
			return 0, false
		}
		prod := term.tokens * term.rate
		if total > math.MaxInt64-prod {
			return 0, false
		}
		total += prod
	}
	return contract.MicroUSD(total/perMillion + (total%perMillion*2)/perMillion), true
}

func (s *snapshot) find(provider, model string, at time.Time) (PriceRow, bool) {
	list := s.byProvider[provider]
	best := -1
	for i, sr := range list {
		if matches(sr.model, model) && (best < 0 || len(sr.model) > len(list[best].model)) {
			best = i
		}
	}
	if best < 0 {
		return PriceRow{}, false
	}
	var row PriceRow
	found := false
	for _, r := range list[best].rows {
		if r.EffectiveFrom.After(at) {
			break
		}
		row, found = r, true
	}
	return row, found
}

func matches(name, model string) bool {
	if !strings.HasPrefix(model, name) {
		return false
	}
	rest := model[len(name):]
	return rest == "" || rest[0] == '-' || rest[0] == ':' || rest[0] == '@'
}
