package router

import (
	"sync"
	"sync/atomic"

	"github.com/ethosagent/meterwise/internal/contract"
)

// resolved is a workspace's router settings as the hook uses them: the mode, the score a call must not exceed and
// its pairs by provider (the workspace's own map, else the defaults).
type resolved struct {
	mode      contract.RouterMode
	threshold int
	byProv    map[string][]contract.RoutePair
}

// match finds the pair whose source model is the call's model, exactly or as a dated or -latest id of it; the
// longest source name wins, so gpt-4o never takes a gpt-4o-mini pair.
func (r *resolved) match(provider, model string) (contract.RoutePair, bool) {
	var best contract.RoutePair
	found := false
	for _, p := range r.byProv[provider] {
		if contract.ModelMatches(p.From, model) && (!found || len(p.From) > len(best.From)) {
			best, found = p, true
		}
	}
	return best, found
}

// memoMax bounds the config memo: workspaces are few, but the key is a string from settings, so it stays capped.
const memoMax = 4096

// configs parses Tenant.Attrs["router"] once per distinct string. The parse of an attr is pure, so an entry never
// goes stale; a changed setting is a new string (wsauth rebuilds the attr when the settings are patched).
type configs struct {
	defaults []contract.RoutePair
	m        sync.Map
	n        atomic.Int64
}

func (c *configs) resolve(attr string) *resolved {
	if attr == "" {
		return offResolved
	}
	if v, ok := c.m.Load(attr); ok {
		return v.(*resolved)
	}
	cfg := contract.ParseRouterAttr(attr)
	r := &resolved{mode: cfg.Mode, threshold: cfg.EffectiveThresholdBp(), byProv: map[string][]contract.RoutePair{}}
	pairs := c.defaults
	if cfg.Map != nil {
		pairs = cfg.Map
	}
	for _, p := range pairs {
		r.byProv[p.Provider] = append(r.byProv[p.Provider], p)
	}
	if c.n.Load() < memoMax {
		if _, loaded := c.m.LoadOrStore(attr, r); !loaded {
			c.n.Add(1)
		}
	}
	return r
}

var offResolved = &resolved{mode: contract.RouterModeOff}
