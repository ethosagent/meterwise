package dataplane

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/config"
)

// Deps are the replaceable parts of the data plane. A nil field takes the Plan
// 01 default: tollgate.NoAuth (dev only; Plan 05 swaps in the /w/{id}
// authenticator), the built-in /openai /anthropic /openrouter prefix resolver,
// Unlimited admission, no hooks and a DiscardSink.
type Deps struct {
	Auth      tollgate.Authenticator
	Resolver  tollgate.ProviderResolver
	Admission tollgate.Admission
	Hooks     []tollgate.Hook
	Sink      tollgate.MetricSink
	// Transport overrides the upstream round tripper (tests point it at a loopback fake).
	Transport http.RoundTripper
	Logger    *slog.Logger
}

// PrivacyModeIgnoredWarning is logged at boot when PRIVACY_MODE=false. tollgate v0.1.0
// accepts Options.DisablePrivacyMode but does not act on it, so the setting changes nothing.
const PrivacyModeIgnoredWarning = "PRIVACY_MODE=false has no effect: tollgate v0.1.0 does not implement a switch for it; privacy behaviour is unchanged"

// Options turns the deps into tollgate.Options. Limits are tollgate's
// defaults. PRIVACY_MODE is accepted but not forwarded: tollgate v0.1.0 ignores
// DisablePrivacyMode (see PrivacyModeIgnoredWarning), so forwarding it would only
// suggest a control that does not exist.
func Options(d Deps) tollgate.Options {
	o := tollgate.Options{
		Auth:      d.Auth,
		Admission: d.Admission,
		Providers: d.Resolver,
		Metrics:   d.Sink,
		Transport: d.Transport,
		Logger:    d.Logger,
		Limits:    tollgate.DefaultLimits(),
	}
	if o.Auth == nil {
		o.Auth = tollgate.NoAuth{}
	}
	if o.Admission == nil {
		o.Admission = tollgate.Unlimited{}
	}
	if o.Metrics == nil {
		o.Metrics = tollgate.DiscardSink{}
	}
	return o
}

// New validates the options and returns the proxy with its hooks registered.
// Mount Handler on LISTEN_PROXY and AdminHandler on LISTEN_ADMIN; call Start
// before serving and Close after the servers have shut down.
func New(cfg config.Config, d Deps) (*tollgate.Proxy, error) {
	if !cfg.PrivacyMode {
		log := d.Logger
		if log == nil {
			log = slog.Default()
		}
		log.Warn(PrivacyModeIgnoredWarning, "key", config.EnvPrivacyMode)
	}
	p, err := tollgate.New(Options(d))
	if err != nil {
		return nil, fmt.Errorf("dataplane: %w", err)
	}
	if len(d.Hooks) > 0 {
		p.Use(d.Hooks...)
	}
	return p, nil
}
