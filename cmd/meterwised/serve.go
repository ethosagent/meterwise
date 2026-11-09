package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/store"
)

const (
	readHeaderTimeout = 5 * time.Second
	apiReadTimeout    = 30 * time.Second
	apiWriteTimeout   = 30 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
	devUserID         = "dev-admin"
)

// instance is everything serve owns for the life of the process.
type instance struct {
	cfg      config.Config
	log      *slog.Logger
	db       *store.Store
	proxy    *tollgate.Proxy
	servers  map[string]*http.Server // proxy, api, admin
	lns      map[string]net.Listener
	shutdown func(context.Context) error // telemetry flush
}

func cmdServe(ctx context.Context, e *env, args []string) int {
	asJSON, _, code := parseFlags(e, "serve", args)
	if code != exitOK {
		return code
	}
	cfg, err := config.Load(e.lookup)
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

// boot wires telemetry, the store, the data plane and the control plane and
// binds the three listeners. Every failure is a typed error with no secrets.
func boot(ctx context.Context, cfg config.Config, log *slog.Logger, deps dataplane.Deps) (*instance, *contract.Error) {
	principals := api.PrincipalResolver(api.StubPrincipalResolver{OrgID: store.OrgID(), UserID: devUserID})
	if cerr := requireDevForStub(cfg.Env, principals); cerr != nil {
		return nil, cerr
	}
	if cerr := requireDevForNoAuth(cfg.Env, deps.Auth); cerr != nil {
		return nil, cerr
	}
	rt := &instance{cfg: cfg, log: log, servers: map[string]*http.Server{}, lns: map[string]net.Listener{}}
	tp, shutdown, err := startTelemetry(ctx, cfg)
	if err != nil {
		cause := scrubURL(err.Error(), cfg.OTLPEndpoint)
		log.Error("telemetry", "err", cause)
		return nil, contract.NewError(contract.CodeInternal, "cannot start telemetry for "+config.EnvOTLPEndpoint, cause)
	}
	rt.shutdown = shutdown
	if rt.db, err = store.Open(ctx, cfg.DatabaseURL); err != nil {
		rt.close(ctx)
		log.Error("database", "err", err)
		return nil, contract.NewError(contract.CodeUnavailable, "cannot connect to the database named by "+config.EnvDatabaseURL)
	}
	if deps.Logger == nil {
		deps.Logger = log
	}
	if rt.proxy, err = dataplane.New(cfg, deps); err != nil {
		rt.close(ctx)
		return nil, contract.NewError(contract.CodeInternal, "cannot build the data plane: "+err.Error())
	}
	apiHandler, err := api.New(api.Deps{
		Log: log, DB: rt.db, Principals: principals,
		Prefs: api.NewMemoryPreferences(), Web: webFS(cfg.WebDir, log), Tracer: tp,
		AppName: cfg.AppName, ProxyPublicHost: cfg.ProxyPublicHost, Version: contract.Version{Version: version, Commit: commit},
	})
	if err != nil {
		rt.close(ctx)
		return nil, contract.NewError(contract.CodeInternal, "cannot build the control plane: "+err.Error())
	}
	rt.servers["proxy"] = &http.Server{Handler: rt.proxy.Handler(), ReadHeaderTimeout: readHeaderTimeout, IdleTimeout: idleTimeout}
	rt.servers["api"] = &http.Server{Handler: apiHandler, ReadHeaderTimeout: readHeaderTimeout, ReadTimeout: apiReadTimeout, WriteTimeout: apiWriteTimeout, IdleTimeout: idleTimeout}
	rt.servers["admin"] = &http.Server{Handler: rt.proxy.AdminHandler(), ReadHeaderTimeout: readHeaderTimeout, WriteTimeout: apiWriteTimeout}
	if cerr := rt.listen(ctx); cerr != nil {
		rt.close(ctx)
		return nil, cerr
	}
	return rt, nil
}

// requireDevForStub refuses to run the stub principal resolver, which signs every
// request in as an org admin, in any environment but dev. It runs before anything is
// opened, so a misconfigured prod process never binds a listener. Plan 02 swaps the
// resolver, after which this guard passes for the real one.
func requireDevForStub(appEnv string, res api.PrincipalResolver) *contract.Error {
	if _, stub := res.(api.StubPrincipalResolver); stub && appEnv != "dev" {
		return contract.NewError(contract.CodeConfigInvalid,
			"the stub principal resolver signs every request in as an admin and runs only with "+config.EnvAppEnv+"=dev",
			config.EnvAppEnv+"="+appEnv+" needs a real principal resolver (Plan 02)")
	}
	return nil
}

// requireDevForNoAuth refuses to run the proxy without a real authenticator outside dev: a nil
// Deps.Auth falls back to tollgate.NoAuth, which admits every caller as one tenant. It is
// independent of the principal guard, so swapping the control-plane resolver (Plan 02) does not
// open the data plane; Plan 05 supplies the real authenticator.
func requireDevForNoAuth(appEnv string, auth tollgate.Authenticator) *contract.Error {
	if _, none := auth.(tollgate.NoAuth); (auth == nil || none) && appEnv != "dev" {
		return contract.NewError(contract.CodeConfigInvalid,
			"the proxy has no authenticator (tollgate.NoAuth admits every caller) and runs only with "+config.EnvAppEnv+"=dev",
			config.EnvAppEnv+"="+appEnv+" needs a real authenticator (Plan 05)")
	}
	return nil
}

// scrubURL removes a URL's credentials from an error message.
func scrubURL(msg, raw string) string {
	if u, err := url.Parse(raw); err == nil && u.User != nil {
		msg = strings.ReplaceAll(msg, u.User.String(), "***")
		if pw, ok := u.User.Password(); ok && pw != "" {
			msg = strings.ReplaceAll(msg, pw, "***")
		}
	}
	return strings.ReplaceAll(msg, raw, "<redacted-url>")
}

// scrubDSN removes everything that identifies the database (credentials, host, port, name) from an
// error message, for the migrate log line.
func scrubDSN(msg, dsn string) string {
	msg = scrubURL(msg, dsn)
	if u, err := url.Parse(dsn); err == nil {
		for _, part := range []string{u.User.Username(), u.Hostname(), u.Port(), strings.TrimPrefix(u.Path, "/")} {
			if part != "" {
				msg = strings.ReplaceAll(msg, part, "***")
			}
		}
	}
	return msg
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
