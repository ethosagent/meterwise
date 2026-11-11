package dataplane

import (
	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/router"
)

// routerHook builds the router hook from ChainOptions.Router. With no capability row there is no target a request
// could be routed to, so the slot stays empty and HookChain skips it: a process whose router tables did not load
// serves exactly the chain it served before the router existed.
func routerHook(o ChainOptions) tollgate.Hook {
	if len(o.Router.Caps) == 0 || len(o.Router.Routes) == 0 {
		return nil
	}
	return router.New(router.Options{Tables: o.Router})
}
