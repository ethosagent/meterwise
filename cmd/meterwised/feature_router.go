package main

import (
	"context"

	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
)

// routerFeature builds the router's tables (pairs, capabilities, priced models) for the data-plane hook and the
// control-plane port for its settings, stats and feedback. Step 0 ships no routing, so it returns empty tables
// and a nil port; the router owner replaces this file wholesale.
func routerFeature(context.Context, *instance, contract.Pricer) (contract.RouterTables, api.RouterReader, error) {
	return contract.RouterTables{}, nil, nil
}
