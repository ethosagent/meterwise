package dataplane

import "github.com/ethosagent/tollgate"

// trimHook builds the trim hook. Step 0 ships no trimming, so the slot is empty and HookChain skips it;
// the trim owner replaces this file wholesale.
func trimHook(ChainOptions) tollgate.Hook { return nil }
