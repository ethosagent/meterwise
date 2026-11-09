package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ethosagent/tollgate"
	"go.opentelemetry.io/otel/trace"

	"github.com/ethosagent/meterwise/internal/access"
	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/tenancy"
)

const (
	readHeaderTimeout = 5 * time.Second
	apiReadTimeout    = 30 * time.Second
	apiWriteTimeout   = 30 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// instance is everything serve owns for the life of the process.
type instance struct {
	cfg      config.Config
	log      *slog.Logger
	db       *store.Store
	svc      *tenancy.Service
	proxy    *tollgate.Proxy
	servers  map[string]*http.Server // proxy, api, admin
	lns      map[string]net.Listener
	shutdown func(context.Context) error // telemetry flush
}

func cmdServe(ctx context.Context, e *env, args []string) int {
	asJSON, _, code := parseFlags(e, "serve", args, nil)
	if code != exitOK {
		return code
	}
	cfg, err := config.Load(e.lookup)
	if err == nil {
		err = cfg.CheckServe()
	}
	if err != nil {
		return fail(e, asJSON, contract.AsError(err), exitFailed)
	}
	log := slog.New(slog.NewJSONHandler(e.stderr, &slog.HandlerOptions{Level: logLevel(cfg.LogLevel)}))
	rt, cerr := boot(ctx, cfg, log, e.deps)
	if cerr != nil {
		return fail(e, asJSON, cerr, exitFailed)
	}
	defer rt.close(ctx)
	addrs := map[string]string{}
	for name, ln := range rt.lns {
		addrs[name] = ln.Addr().String()
	}
	ok(e, asJSON, addrs)
	log.Info("serving", "proxy", addrs["proxy"], "api", addrs["api"], "admin", addrs["admin"], "env", cfg.Env, "privacy_mode", cfg.PrivacyMode)
	if err := rt.serve(ctx); err != nil {
		log.Error("serve failed", "err", err)
		return exitFailed
	}
	log.Info("stopped")
	return exitOK
}

// boot wires telemetry, the store, identity, the data plane and the control plane and
// binds the three listeners. Every failure is a typed error with no secrets. Both
// production guards run first, before anything is opened, so a misconfigured process
// never binds a listener: the principal guard is the stub resolver's constructor
// (it cannot be built outside dev) and the proxy guard is requireDevForNoAuth.
func boot(ctx context.Context, cfg config.Config, log *slog.Logger, deps dataplane.Deps) (*instance, *contract.Error) {
	rt := &instance{cfg: cfg, log: log, servers: map[string]*http.Server{}, lns: map[string]net.Listener{}}
	id, cerr := newIdentity(cfg, rt.devSession)
	if cerr != nil {
		return nil, cerr
	}
	if cerr := requireDevForNoAuth(cfg.Env, deps.Auth); cerr != nil {
		return nil, cerr
	}
	tp, shutdown, err := startTelemetry(ctx, cfg)
	if err != nil {
		log.Error("telemetry", "err", err.Error())
		return nil, contract.NewError(contract.CodeInternal, "cannot start telemetry for "+config.EnvOTLPEndpoint, err.Error())
	}
	rt.shutdown = shutdown
	if rt.db, err = store.Open(ctx, cfg.DatabaseURL); err != nil {
		rt.close(ctx)
		log.Error("database", "class", string(store.ClassOf(err)), "err", err.Error())
		return nil, contract.NewError(contract.CodeUnavailable, "cannot connect to the database named by "+config.EnvDatabaseURL)
	}
	rt.svc = tenancy.New(rt.db, id.provider, tenancy.Config{MaxWorkspaces: cfg.MaxWorkspaces, Log: log})
	if err := rt.svc.EnsureDefaults(ctx); err != nil {
		rt.close(ctx)
		log.Error("default workspace", "err", err.Error())
		return nil, contract.NewError(contract.CodeUnavailable, "cannot prepare the organization; run `meterwised migrate` first")
	}
	if deps.Logger == nil {
		deps.Logger = log
	}
	if rt.proxy, err = dataplane.New(cfg, deps); err != nil {
		rt.close(ctx)
		return nil, contract.NewError(contract.CodeInternal, "cannot build the data plane: "+err.Error())
	}
	apiHandler, err := rt.apiHandler(id, tp)
	if err != nil {
		rt.close(ctx)
		return nil, contract.NewError(contract.CodeInternal, "cannot build the control plane: "+err.Error())
	}
	if cerr := rt.mount(ctx, apiHandler); cerr != nil {
		rt.close(ctx)
		return nil, cerr
	}
	return rt, nil
}

// apiHandler builds the control plane around the resolver identity chose: the dev stub when
// there is one, otherwise the Clerk resolver over the tenancy service.
func (rt *instance) apiHandler(id identitySetup, tp trace.TracerProvider) (http.Handler, error) {
	principals := id.resolver
	if principals == nil {
		principals = api.ClerkPrincipalResolver{Provider: id.provider, Sessions: rt.svc}
	}
	return api.New(api.Deps{
		Log: rt.log, DB: rt.db, Principals: principals, Tenancy: rt.svc, Authz: access.NewAuthorizer(rt.db, rt.svc.Directory()), Access: access.NewService(rt.db, store.OrgID()), Webhooks: id.webhooks,
		Web: webFS(rt.cfg.WebDir, rt.log), Tracer: tp,
		AppName: rt.cfg.AppName, ProxyPublicHost: rt.cfg.ProxyPublicHost, Version: contract.Version{Version: version, Commit: commit},
	})
}

// mount creates the three servers and binds their listeners.
func (rt *instance) mount(ctx context.Context, apiHandler http.Handler) *contract.Error {
	rt.servers["proxy"] = &http.Server{Handler: rt.proxy.Handler(), ReadHeaderTimeout: readHeaderTimeout, IdleTimeout: idleTimeout}
	rt.servers["api"] = &http.Server{Handler: apiHandler, ReadHeaderTimeout: readHeaderTimeout, ReadTimeout: apiReadTimeout, WriteTimeout: apiWriteTimeout, IdleTimeout: idleTimeout}
	rt.servers["admin"] = &http.Server{Handler: rt.proxy.AdminHandler(), ReadHeaderTimeout: readHeaderTimeout, WriteTimeout: apiWriteTimeout}
	return rt.listen(ctx)
}

// requireDevForNoAuth refuses to run the proxy without a real authenticator outside dev: a nil
// Deps.Auth falls back to tollgate.NoAuth, which admits every caller as one tenant. tollgate.NoAuth
// is a third-party type, so the check is explicit: the nil interface, the value and the pointer
// form all count as "no authenticator". It is independent of the principal guard, so a real
// control-plane resolver does not open the data plane.
func requireDevForNoAuth(appEnv string, auth tollgate.Authenticator) *contract.Error {
	if appEnv == "dev" {
		return nil
	}
	switch auth.(type) {
	case nil, tollgate.NoAuth, *tollgate.NoAuth:
		return contract.NewError(contract.CodeConfigInvalid,
			"the proxy has no authenticator (tollgate.NoAuth admits every caller) and runs only with "+config.EnvAppEnv+"=dev",
			config.EnvAppEnv+"="+appEnv+" needs a proxy authenticator other than tollgate.NoAuth")
	}
	return nil
}

// listen binds every listener and reports all that fail, not just the first.
func (rt *instance) listen(ctx context.Context) *contract.Error {
	var lc net.ListenConfig
	var problems []string
	for _, l := range []struct{ name, key, addr string }{
		{"proxy", config.EnvListenProxy, rt.cfg.ListenProxy},
		{"api", config.EnvListenAPI, rt.cfg.ListenAPI},
		{"admin", config.EnvListenAdmin, rt.cfg.ListenAdmin},
	} {
		ln, err := lc.Listen(ctx, "tcp", l.addr)
		if err != nil {
			problems = append(problems, l.key+": cannot listen on "+l.addr)
			continue
		}
		rt.lns[l.name] = ln
	}
	if len(problems) > 0 {
		return contract.NewError(contract.CodeInternal, "cannot bind every listener", problems...)
	}
	return nil
}

// serve runs the three servers until ctx ends or one fails, then drains them.
func (rt *instance) serve(ctx context.Context) error {
	if err := rt.proxy.Start(ctx); err != nil {
		return err
	}
	errc := make(chan error, len(rt.servers))
	for name, srv := range rt.servers {
		go func() { errc <- srv.Serve(rt.lns[name]) }()
	}
	var first error
	select {
	case first = <-errc:
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for _, srv := range rt.servers {
		wg.Go(func() { _ = srv.Shutdown(sctx) })
	}
	wg.Wait()
	if errors.Is(first, http.ErrServerClosed) {
		first = nil
	}
	return first
}

// close releases everything boot acquired, in reverse order. The proxy closes
// after the servers so every call's metric is queued first.
func (rt *instance) close(ctx context.Context) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	for _, ln := range rt.lns {
		_ = ln.Close()
	}
	if rt.proxy != nil {
		if err := rt.proxy.Close(cctx); err != nil {
			rt.log.Warn("proxy close", "err", err)
		}
	}
	if rt.db != nil {
		rt.db.Close()
	}
	if rt.shutdown != nil {
		if err := rt.shutdown(cctx); err != nil {
			rt.log.Warn("telemetry flush", "err", err)
		}
	}
}

// webFS returns the built web app, or nil (with a warning) when it is absent.
func webFS(dir string, log *slog.Logger) fs.FS {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		log.Warn("web app not found; the API still serves", "dir", dir)
		return nil
	}
	return os.DirFS(dir)
}

func logLevel(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return l
}
