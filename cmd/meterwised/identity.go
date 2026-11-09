package main

import (
	"context"
	"time"

	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/identity"
)

// devIdentity is the one user the dev-only resolver signs in as.
var devIdentity = contract.Identity{Subject: "dev-admin", Email: "dev-admin@meterwise.local", EmailVerified: true, Name: "Dev Admin"}

// identitySetup is the identity half of boot: the provider, the webhook verifier
// and, in dev without Clerk, the stub resolver (nil otherwise).
type identitySetup struct {
	provider contract.IdentityProvider
	webhooks api.WebhookVerifier
	resolver api.PrincipalResolver
}

// newIdentity builds identity from the config. With Clerk configured it is the
// real adapter. Without it, only the dev-only stub resolver can run: its
// constructor fails outside dev, so a production process cannot hold it. It
// makes no network call and opens nothing.
func newIdentity(cfg config.Config, devSession func(context.Context) (contract.Session, error)) (identitySetup, *contract.Error) {
	if !cfg.Clerk.Configured() {
		stub, err := api.NewStubPrincipalResolver(cfg.Env, devSession)
		if err != nil {
			return identitySetup{}, contract.NewError(contract.CodeConfigInvalid,
				"the control plane needs a non-stub principal resolver: "+err.Error(),
				config.EnvAppEnv+"="+cfg.Env+" needs "+config.EnvClerkIssuer+" and "+config.EnvClerkJWKSURL)
		}
		return identitySetup{provider: identity.Disabled{}, resolver: stub}, nil
	}
	provider, err := identity.NewClerk(identity.ClerkConfig{
		Issuer: cfg.Clerk.Issuer, JWKSURL: cfg.Clerk.JWKSURL, SecretKey: cfg.Clerk.SecretKey, APIURL: cfg.Clerk.APIURL,
		RedirectURL: acceptInviteURL(cfg.AppURL),
	})
	if err != nil {
		return identitySetup{}, contract.NewError(contract.CodeConfigInvalid, "cannot set up the identity provider", err.Error())
	}
	set := identitySetup{provider: provider}
	if cfg.Clerk.WebhookSecret != "" {
		v, err := identity.NewWebhookVerifier(cfg.Clerk.WebhookSecret, time.Now)
		if err != nil {
			return identitySetup{}, contract.NewError(contract.CodeConfigInvalid, "cannot set up the webhook verifier", err.Error())
		}
		set.webhooks = v
	}
	return set, nil
}

func acceptInviteURL(appURL string) string {
	if appURL == "" {
		return ""
	}
	return appURL + "/accept-invite"
}

// devSession is the stub resolver's loader: the dev admin, mirrored and made an admin member.
func (rt *instance) devSession(ctx context.Context) (contract.Session, error) {
	sess, err := rt.svc.Resolve(ctx, devIdentity)
	if err != nil {
		return contract.Session{}, err
	}
	if !sess.Member {
		if err := rt.svc.EnsureMember(ctx, sess.User.ID, contract.RoleAdmin); err != nil {
			return contract.Session{}, err
		}
		return rt.svc.Resolve(ctx, devIdentity)
	}
	return sess, nil
}
