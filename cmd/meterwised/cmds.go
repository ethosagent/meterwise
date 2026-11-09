package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

func cmdVersion(_ context.Context, e *env, args []string) int {
	asJSON, _, code := parseFlags(e, "version", args, nil)
	if code != exitOK {
		return code
	}
	v := contract.Version{Version: version, Commit: commit}
	if asJSON {
		return ok(e, true, map[string]string{"version": v.Version, "commit": v.Commit, "go": runtime.Version()})
	}
	return ok(e, false, map[string]string{"version": v.Version, "commit": v.Commit})
}

func cmdConfig(_ context.Context, e *env, args []string) int {
	asJSON, rest, code := parseFlags(e, "config", args, nil)
	if code != exitOK {
		return code
	}
	if len(rest) != 1 || rest[0] != "check" {
		return fail(e, asJSON, contract.NewError(contract.CodeInvalidRequest, "usage: meterwised config check [-json]"), exitUsage)
	}
	cfg, err := config.Load(e.lookup)
	if err == nil {
		err = cfg.CheckServe()
	}
	if err != nil {
		return fail(e, asJSON, contract.AsError(err), exitFailed)
	}
	return ok(e, asJSON, cfg.Redacted())
}

// cmdMigrate applies pending migrations (default, or "up") or lists them ("status").
func cmdMigrate(ctx context.Context, e *env, args []string) int {
	asJSON, rest, code := parseFlags(e, "migrate", args, nil)
	if code != exitOK {
		return code
	}
	action := "up"
	if len(rest) == 1 {
		action = rest[0]
	}
	if len(rest) > 1 || (action != "up" && action != "status") {
		return fail(e, asJSON, contract.NewError(contract.CodeInvalidRequest, "usage: meterwised migrate [up|status] [-json]"), exitUsage)
	}
	cfg, err := config.Load(e.lookup)
	if err != nil {
		return fail(e, asJSON, contract.AsError(err), exitFailed)
	}
	var ms []store.Migration
	if action == "up" {
		ms, err = store.Migrate(ctx, cfg.DatabaseURL)
	} else {
		ms, err = store.Status(ctx, cfg.DatabaseURL)
	}
	if err != nil {
		// The store returns a fixed message and a class, never driver text, so the log line is safe as is.
		slog.New(slog.NewJSONHandler(e.stderr, nil)).Error("migrate", "action", action, "class", string(store.ClassOf(err)), "err", err.Error())
		return fail(e, asJSON, contract.NewError(contract.CodeUnavailable, "migrate "+action+" failed; see the log for the cause"), exitFailed)
	}
	if ms == nil {
		ms = []store.Migration{}
	}
	if asJSON {
		return ok(e, true, map[string]any{"action": action, "migrations": ms})
	}
	for _, m := range ms {
		state := "pending"
		if m.Applied {
			state = "applied"
		}
		_, _ = fmt.Fprintf(e.stdout, "%04d %-8s %s\n", m.Version, state, m.Source)
	}
	if len(ms) == 0 {
		_, _ = io.WriteString(e.stdout, "nothing to do\n")
	}
	return exitOK
}
