package main

import (
	"context"

	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/store"
)

// memoryChain fills the memory hook of the demo's hook chain (with its mock model twin on loopback). Step 0 ships no
// memory, so it leaves the slot empty; the memory owner replaces this file wholesale.
func memoryChain(context.Context, *store.Store, *dataplane.ChainOptions) error { return nil }

// memoryRounds plays the memory scenarios after the base rounds; there is nothing to play yet.
func memoryRounds(context.Context, *client, config) error { return nil }
