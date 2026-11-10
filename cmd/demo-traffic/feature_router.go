package main

import (
	"context"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/store"
)

// routerChain fills the router tables of the demo's hook chain (priced from the same table the ledger uses). Step 0
// ships no routing, so it leaves them empty; the router owner replaces this file wholesale.
func routerChain(context.Context, *store.Store, contract.Pricer, *dataplane.ChainOptions) error {
	return nil
}

// routerRounds plays the router scenarios after the base rounds; there is nothing to play yet.
func routerRounds(context.Context, *client, config) error { return nil }
