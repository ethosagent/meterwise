package wsauth

import (
	"context"

	"github.com/ethosagent/tollgate"
)

// Hook copies the authenticator's retired-URL flag into Call.Meta (an Authenticator cannot write
// Meta: the call is created after authentication). It does no I/O and never short-circuits.
type Hook struct{ tollgate.NoopHook }

// Name identifies the hook in logs and metrics.
func (Hook) Name() string { return "wsauth" }

// Before sets Meta["url_retired"]="true" for a call that came in through a retired URL.
func (Hook) Before(_ context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	if c.Tenant.Attrs[MetaURLRetired] == "true" {
		if c.Meta == nil {
			c.Meta = map[string]any{}
		}
		c.Meta[MetaURLRetired] = "true"
	}
	return nil, nil
}
