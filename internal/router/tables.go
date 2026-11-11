package router

import (
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

// DefaultMaxTools is the most tool definitions a request may carry and still be routed to a model whose capability
// row states no maximum (max_tools NULL: the provider's page says the model supports tool use without a limit).
// It is the router's own policy, not a provider claim: small models pick worse among many tools, so a request that
// carries more than this stays on the model it asked for. A row with a number (0 = no tool use) keeps that number.
const DefaultMaxTools = 32

// CapRow is one router_model_capabilities row as the store reads it; a nil MaxTools is NULL in the table.
type CapRow = contract.RouterCapRow

// Dropped is a default pair BuildTables left out and why.
type Dropped struct {
	Pair   contract.RoutePair
	Reason string
}

// The reasons a default pair is dropped.
const (
	DropFromUnpriced = "source model has no price"
	DropToUnpriced   = "target model has no price"
	DropNoCaps       = "target model has no capability row"
)

// TableKey is the key of RouterTables.Caps and .Priced.
func TableKey(provider, model string) string { return provider + "/" + model }

// BuildTables assembles the router's tables once, at boot. A default pair is kept only when both of its models are
// priced at now (R10) and the target has a capability row; every other pair is returned in dropped with its reason
// so the caller can log it. Priced covers the models of every default pair and every capability row, so a workspace
// map that reuses them still prices. A NULL max_tools becomes DefaultMaxTools. The pricer is only asked, never
// stored, so a later price change needs a restart to reach the router.
func BuildTables(routes []contract.RoutePair, caps []CapRow, pricer contract.Pricer, now time.Time) (contract.RouterTables, []Dropped) {
	t := contract.RouterTables{Caps: map[string]contract.ModelCaps{}, Priced: map[string]bool{}}
	priced := func(provider, model string) bool {
		k := TableKey(provider, model)
		if v, seen := t.Priced[k]; seen {
			return v
		}
		_, ok := pricer.Cost(provider, model, contract.Usage{Input: 1}, now)
		t.Priced[k] = ok
		return ok
	}
	for _, c := range caps {
		mt := DefaultMaxTools
		if c.MaxTools != nil {
			mt = max(*c.MaxTools, 0)
		}
		t.Caps[TableKey(c.Provider, c.Model)] = contract.ModelCaps{MaxTools: mt, JSONSchema: c.JSONSchema, Vision: c.Vision, Reasoning: c.Reasoning}
		priced(c.Provider, c.Model)
	}
	var dropped []Dropped
	for _, p := range routes {
		okFrom, okTo := priced(p.Provider, p.From), priced(p.Provider, p.To)
		_, hasCaps := t.Caps[TableKey(p.Provider, p.To)]
		switch {
		case !okFrom:
			dropped = append(dropped, Dropped{p, DropFromUnpriced})
		case !okTo:
			dropped = append(dropped, Dropped{p, DropToUnpriced})
		case !hasCaps:
			dropped = append(dropped, Dropped{p, DropNoCaps})
		default:
			t.Routes = append(t.Routes, p)
		}
	}
	return t, dropped
}
