package dataplane

import "github.com/ethosagent/tollgate"

// routerHook builds the router hook from ChainOptions.Router. Step 0 ships no routing, so the slot is
// empty and HookChain skips it; the router owner replaces this file wholesale.
func routerHook(ChainOptions) tollgate.Hook { return nil }
