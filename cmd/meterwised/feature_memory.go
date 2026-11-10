package main

import (
	"context"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
)

// memoryFeature builds the memory hook for the data-plane chain, the control-plane port and the status source for
// /statusz. Step 0 ships no memory, so all three are nil; the memory owner replaces this file wholesale.
func memoryFeature(context.Context, *instance) (tollgate.Hook, api.MemoryAPI, contract.StatusSource, error) {
	return nil, nil, nil, nil
}
