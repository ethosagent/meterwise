package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/ledger"
	"github.com/ethosagent/meterwise/internal/savings"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

// roleRunbook is where the least-privilege role is set up.
const roleRunbook = "docs/runbooks/database-roles.md"

// requireLeastPrivilegeRole refuses a database role that could rewrite the audit log: outside
// APP_ENV=dev the app must not run as a superuser or a member of one (it bypasses every
// permission, including the append-only trigger's), nor with BYPASSRLS directly or through
// membership, nor as the owner of audit_log (an owner may drop the trigger). Migrations run as the
// owner role; the app runs as meterwise_app.
func requireLeastPrivilegeRole(appEnv string, rc store.RoleCheck) *contract.Error {
	if appEnv == "dev" {
		return nil
	}
	var why string
	switch {
	case rc.Superuser:
		why = fmt.Sprintf("the database role %q is a superuser", rc.Role)
	case rc.BypassRLS:
		why = fmt.Sprintf("the database role %q bypasses row-level security", rc.Role)
	case rc.OwnsAuditLog:
		why = fmt.Sprintf("the database role %q owns audit_log", rc.Role)
	default:
		return nil
	}
	return contract.NewError(contract.CodeConfigInvalid,
		why+": run the app as the least-privilege role meterwise_app and migrate as the owner",
		"see "+roleRunbook, config.EnvAppEnv+" other than dev enforces this")
}

// buildPlane completes the data-plane deps and builds the proxy.
func (rt *instance) buildPlane(ctx context.Context, deps dataplane.Deps) *contract.Error {
	if deps.Logger == nil {
		deps.Logger = rt.log
	}
	deps, err := rt.completeDeps(ctx, deps)
	if err != nil {
		rt.log.Error("data plane deps", "err", err.Error())
		return contract.NewError(contract.CodeInternal, "cannot prepare the data plane")
	}
	if rt.proxy, err = dataplane.New(rt.cfg, deps); err != nil {
		return contract.NewError(contract.CodeInternal, "cannot build the data plane: "+err.Error())
	}
	return nil
}

// checkRole applies the least-privilege guard to the role the pool connected as.
func (rt *instance) checkRole(ctx context.Context) *contract.Error {
	rc, err := rt.db.CheckRole(ctx)
	if err != nil {
		rt.log.Error("database role", "err", err.Error())
		return contract.NewError(contract.CodeUnavailable, "cannot read the database role")
	}
	return requireLeastPrivilegeRole(rt.cfg.Env, rc)
}

// completeDeps fills what the data plane needs from the process: the /w/{id} authenticator over
// the cached directory, the workspace resolver, per-workspace admission and the wsauth hook (ahead
// of the injected hooks). A field the caller set is kept, so tests and the integration build can
// replace any piece. The directory and admission join the /statusz sources.
func (rt *instance) completeDeps(ctx context.Context, d dataplane.Deps) (dataplane.Deps, error) {
	dir := rt.svc.Directory()
	dir.SetView(wsauth.Resolve)
	dev := rt.cfg.Env == "dev"
	if d.Auth == nil {
		auth := wsauth.New(dir, 0)
		if dev {
			t, err := rt.devTenant(ctx)
			if err != nil {
				return d, err
			}
			auth = auth.WithDevTenant(t)
		}
		d.Auth = auth
	}
	if d.Resolver == nil {
		d.Resolver = dataplane.NewWorkspaceResolver(nil, dev)
	}
	if d.Admission == nil {
		limit := rt.cfg.MaxConcurrentPerWorkspace
		if limit <= 0 { // a Config built by hand rather than loaded from the environment
			limit = config.DefaultMaxConcurrentPerWorkspace
		}
		adm, err := dataplane.NewAdmission(limit, dataplane.QueueTimeout)
		if err != nil {
			return d, err
		}
		d.Admission = adm
		d.Sources = append(d.Sources, adm)
	}
	d.Sources = append(d.Sources, dir)
	led, pricer, err := rt.startLedger(ctx)
	if err != nil {
		return d, err
	}
	d.Sources = append(d.Sources, led)
	opts := dataplane.ChainOptions{PrivacyMode: rt.cfg.PrivacyMode, HistoryMaxBytes: rt.cfg.HistoryMaxBytes}
	if err := rt.buildFeatures(ctx, pricer, &opts, &d); err != nil {
		return d, err
	}
	// Order is load-bearing (plan 06 R2, plan 09-11 R4); dataplane.HookChain owns it.
	hooks, srcs := dataplane.HookChain(opts, d.Hooks, led)
	d.Sources = append(d.Sources, srcs...)
	d.Hooks = hooks
	rt.sources = d.Sources
	return d, nil
}

// startLedger builds the price snapshot (loaded once now, then refreshed every five minutes) and the
// ledger hook that turns every finished call into rows. The hook's batcher stops in close, after the
// proxy, so every queued call is flushed. A missing or empty pricing table is logged, not fatal: calls
// are then recorded with a null cost until the next refresh finds prices.
func (rt *instance) startLedger(ctx context.Context) (*ledger.Hook, contract.Pricer, error) {
	pricer := savings.NewTablePricer(nil)
	loader := &ledger.PriceLoader{Source: rt.db, Pricer: pricer, Log: rt.log}
	if err := loader.Load(ctx); err != nil {
		rt.log.Warn("pricing table not loaded; calls are recorded unpriced until it is", "err", err.Error())
	}
	pctx, stop := context.WithCancel(context.WithoutCancel(ctx))
	rt.stopPricing = stop
	go loader.Run(pctx)
	rt.db.SetLogger(rt.log)
	hook, err := ledger.New(ctx, ledger.Options{Writer: rt.db, Pricer: pricer, Estimator: savings.Estimate, PrivacyMode: &rt.cfg.PrivacyMode, Log: rt.log})
	if err != nil {
		return nil, nil, fmt.Errorf("ledger: %w", err)
	}
	rt.ledger = hook
	return hook, pricer, nil
}

// buildFeatures asks the trim, router and memory seams for what they add: the router tables and the memory hook for
// the chain options, the memory status source for /statusz, and the control-plane ports the API serves through. A
// seam that returns nothing leaves its feature off.
func (rt *instance) buildFeatures(ctx context.Context, pricer contract.Pricer, opts *dataplane.ChainOptions, d *dataplane.Deps) error {
	rt.trim = trimFeature(rt)
	tables, router, err := routerFeature(ctx, rt, pricer)
	if err != nil {
		return fmt.Errorf("router: %w", err)
	}
	opts.Router, rt.router = tables, router
	hook, memory, src, err := memoryFeature(ctx, rt)
	if err != nil {
		return fmt.Errorf("memory: %w", err)
	}
	opts.Memory, rt.memory = hook, memory
	if src != nil {
		d.Sources = append(d.Sources, src)
	}
	return nil
}

// devTenant is the tenant a bare /{provider}/ path serves in dev: the org's default workspace as
// it is at boot (settings changes apply after a restart; the /w/{id} path is live).
func (rt *instance) devTenant(ctx context.Context) (tollgate.Tenant, error) {
	list, err := rt.svc.Directory().List(ctx, contract.Principal{OrgID: store.OrgID()})
	if err != nil {
		return tollgate.Tenant{}, fmt.Errorf("dev workspace: %w", err)
	}
	for _, ws := range list {
		if ws.IsDefault {
			return wsauth.Resolve(ws).(*wsauth.Resolved).Tenant, nil
		}
	}
	return tollgate.Tenant{}, fmt.Errorf("dev workspace: the default workspace does not exist")
}

// redacting returns log with every /w/{id} in any message or attribute replaced (L4).
func redacting(log *slog.Logger) *slog.Logger {
	return slog.New(wsauth.RedactingHandler(log.Handler()))
}
