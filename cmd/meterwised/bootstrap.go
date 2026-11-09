package main

import (
	"context"
	"flag"
	"strconv"
	"strings"
	"time"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/tenancy"
)

// cmdBootstrapAdmin creates the first admin invitation: it opens the store,
// makes sure the default workspace exists and invites the email as admin (a
// Clerk invitation when Clerk is configured, a database-only one in dev). It is
// idempotent: an open invitation for the email is reported, not duplicated.
func cmdBootstrapAdmin(ctx context.Context, e *env, args []string) int {
	var email string
	asJSON, _, code := parseFlags(e, "bootstrap-admin", args, func(fs *flag.FlagSet) {
		fs.StringVar(&email, "email", "", "address to invite as admin")
	})
	if code != exitOK {
		return code
	}
	if strings.TrimSpace(email) == "" {
		return fail(e, asJSON, contract.NewError(contract.CodeInvalidRequest, "usage: meterwised bootstrap-admin --email <address> [-json]"), exitUsage)
	}
	cfg, err := config.Load(e.lookup)
	if err != nil {
		return fail(e, asJSON, contract.AsError(err), exitFailed)
	}
	id, cerr := newBootstrapIdentity(cfg)
	if cerr != nil {
		return fail(e, asJSON, cerr, exitFailed)
	}
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fail(e, asJSON, contract.NewError(contract.CodeUnavailable, "cannot connect to the database named by "+config.EnvDatabaseURL), exitFailed)
	}
	defer db.Close()
	svc := tenancy.New(db, id.provider, tenancy.Config{MaxWorkspaces: cfg.MaxWorkspaces})
	if err := svc.EnsureDefaults(ctx); err != nil {
		return fail(e, asJSON, contract.NewError(contract.CodeUnavailable, "cannot prepare the organization; run `meterwised migrate` first"), exitFailed)
	}
	inv, created, err := svc.BootstrapAdmin(ctx, email)
	if err != nil {
		return fail(e, asJSON, contract.AsError(err), exitFailed)
	}
	provider := "none (Clerk is not configured; sign up in dev, or set the CLERK_* variables)"
	if inv.ClerkInvitationID != "" {
		provider = "sent (" + inv.ClerkInvitationID + ")"
	}
	return ok(e, asJSON, map[string]string{
		"invitationId": inv.ID, "email": inv.Email, "role": string(inv.Role), "status": string(inv.StatusAt(time.Now())),
		"created": strconv.FormatBool(created), "providerInvitation": provider, "expiresAt": inv.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// newBootstrapIdentity is newIdentity without the principal side: the CLI only
// needs the provider, in any environment.
func newBootstrapIdentity(cfg config.Config) (identitySetup, *contract.Error) {
	if cfg.Clerk.Configured() {
		return newIdentity(cfg, nil)
	}
	return newIdentity(config.Config{Env: "dev"}, nil)
}
