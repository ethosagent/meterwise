package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/ethosagent/tollgate"

	appconfig "github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/ledger"
)

// tenantAuth hands every request the one workspace's tenant. The tenant is built by wsauth.Resolve from
// the stored workspace, so its attributes (monitor config, tenant_ok) are the ones serve's /w/{id} path
// would give.
type tenantAuth struct{ tenant tollgate.Tenant }

func (a tenantAuth) Authenticate(context.Context, *http.Request) (tollgate.Tenant, error) {
	return a.tenant, nil
}

// providersAt is dataplane's provider table with every upstream replaced by the loopback fake.
func providersAt(base *url.URL) map[string]tollgate.Provider {
	m := dataplane.DefaultProviders()
	for name, p := range m {
		p.BaseURL = base
		m[name] = p
	}
	return m
}

// startProxy builds the proxy exactly as serve does (dataplane.New over dataplane.HookChain: wsauth,
// history, monitors, ledger) in front of the fake upstream. The resolver is the workspace resolver in
// dev mode, so the bare /openai and /anthropic paths work.
func startProxy(ctx context.Context, upstreamURL string, tenant tollgate.Tenant, led *ledger.Hook) (*tollgate.Proxy, *httptest.Server, error) {
	base, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, nil, err
	}
	cfg := appconfig.Config{PrivacyMode: true, HistoryMaxBytes: appconfig.DefaultHistoryMaxBytes}
	hooks, _ := dataplane.HookChain(dataplane.ChainOptions{PrivacyMode: cfg.PrivacyMode, HistoryMaxBytes: cfg.HistoryMaxBytes}, nil, led)
	p, err := dataplane.New(cfg, dataplane.Deps{
		Auth:     tenantAuth{tenant},
		Resolver: dataplane.NewWorkspaceResolver(providersAt(base), true),
		Hooks:    hooks,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("proxy: %w", err)
	}
	if err := p.Start(ctx); err != nil {
		return nil, nil, err
	}
	return p, httptest.NewServer(p.Handler()), nil
}
