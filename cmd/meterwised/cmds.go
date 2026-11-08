package main

import (
	"context"
	"runtime"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
)

func cmdVersion(_ context.Context, e *env, args []string) int {
	asJSON, _, code := parseFlags(e, "version", args)
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
	asJSON, rest, code := parseFlags(e, "config", args)
	if code != exitOK {
		return code
	}
	if len(rest) != 1 || rest[0] != "check" {
		return fail(e, asJSON, contract.NewError(contract.CodeInvalidRequest, "usage: meterwised config check [-json]"), exitUsage)
	}
	cfg, err := config.Load(e.lookup)
	if err != nil {
		return fail(e, asJSON, contract.AsError(err), exitFailed)
	}
	return ok(e, asJSON, cfg.Redacted())
}

// cmdUnavailable answers the typed not_available error for a subcommand whose
// dependencies (the store) are not part of this build.
func cmdUnavailable(name string) func(context.Context, *env, []string) int {
	return func(_ context.Context, e *env, args []string) int {
		asJSON, _, code := parseFlags(e, name, args)
		if code != exitOK {
			return code
		}
		return fail(e, asJSON, contract.NewError(contract.CodeNotAvailable, name+" needs the store package, which this build does not include"), exitFailed)
	}
}
