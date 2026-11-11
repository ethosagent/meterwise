package main

import (
	"context"
	"time"

	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/router"
	"github.com/ethosagent/meterwise/internal/store"
)

// routerFeature loads the router's tables once at boot: the default route pairs and capability rows from the
// database, priced against the same snapshot the ledger uses. A default pair is kept only when both models are
// priced and the target has a capability row; every dropped pair is logged with its reason (R10). The tables feed
// the data-plane hook, and the same pairs are the "defaults" the settings API shows. Prices that change later reach
// the router at the next restart.
func routerFeature(ctx context.Context, rt *instance, pricer contract.Pricer) (contract.RouterTables, api.RouterReader, error) {
	routes, caps, err := rt.db.RouterRows(ctx)
	if err != nil {
		return contract.RouterTables{}, nil, err
	}
	tables, dropped := router.BuildTables(routes, caps, pricer, time.Now())
	for _, d := range dropped {
		rt.log.Warn("router pair dropped", "provider", d.Pair.Provider, "from", d.Pair.From, "to", d.Pair.To, "reason", d.Reason)
	}
	rt.log.Info("router tables loaded", "pairs", len(tables.Routes), "dropped", len(dropped), "models_with_capabilities", len(tables.Caps))
	return tables, store.NewRouterReader(rt.db, tables.Routes), nil
}
