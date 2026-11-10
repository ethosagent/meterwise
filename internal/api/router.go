package api

import (
	"context"

	"github.com/ethosagent/meterwise/internal/contract"
)

// RouterReader is what the control plane reads and writes for the router; the router feature's store implements it
// and cmd wires it. Every call but Defaults is scoped by the workspace id the gateway authorized. A nil port answers
// the stats and feedback routes with not_available and the settings with no model pairs. The handler sets
// RouterStats.Mode from the workspace's settings and builds Effective (the workspace map, else the defaults).
type RouterReader interface {
	// Defaults are the priced default pairs.
	Defaults(ctx context.Context) ([]contract.RoutePair, error)
	// Suggestions are pairs inferred from the models the workspace's calls used in the last 30 days.
	Suggestions(ctx context.Context, workspaceID string) ([]contract.RoutePair, error)
	Stats(ctx context.Context, workspaceID string, r contract.SavingsRange) (contract.RouterStats, error)
	// SetFeedback sets or clears (nil) the verdict on a call; a call outside the workspace is not_found.
	SetFeedback(ctx context.Context, workspaceID, callID, userID string, verdict *string) (contract.RouterFeedbackView, error)
}

// registerRouter wires the router routes. The gateway has authorized the workspace and the permission.
func (s *server) registerRouter() {
	register(s, "GET", "/api/workspaces/{id}/router", s.getRouter)
	register(s, "PATCH", "/api/workspaces/{id}/router", s.patchRouter)
	register(s, "GET", "/api/workspaces/{id}/router/stats", s.routerStats)
	register(s, "POST", "/api/workspaces/{id}/calls/{callId}/router-feedback", s.routerFeedback)
}

func (s *server) routerReader() (RouterReader, error) {
	if s.Router == nil {
		return nil, contract.Unavailable("the router", "")
	}
	return s.Router, nil
}

// routerSettings is the settings response around cfg: the port's pairs, or empty lists while no port is wired.
func (s *server) routerSettings(c Call, cfg contract.RouterConfig) (contract.RouterSettings, error) {
	out := contract.RouterSettings{Config: cfg, Defaults: []contract.RoutePair{}, Suggestions: []contract.RoutePair{}, Effective: []contract.RoutePair{}}
	if s.Router == nil {
		return out, nil
	}
	defaults, err := s.Router.Defaults(c.Ctx)
	if err != nil {
		return out, err
	}
	suggestions, err := s.Router.Suggestions(c.Ctx, c.Path("id"))
	if err != nil {
		return out, err
	}
	out.Defaults, out.Suggestions = nonNil(defaults), nonNil(suggestions)
	out.Effective = out.Defaults
	if cfg.Map != nil { // a workspace map replaces the defaults wholesale (R10)
		out.Effective = nonNil(cfg.Map)
	}
	return out, nil
}

// nonNil turns a nil list into an empty one so it serializes as [].
func nonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

func (s *server) getRouter(c Call, _ none) (contract.RouterSettings, error) {
	cfg, err := s.Tenancy.RouterConfig(c.Ctx, c.Principal, c.Path("id"))
	if err != nil {
		return contract.RouterSettings{}, err
	}
	return s.routerSettings(c, cfg)
}

func (s *server) patchRouter(c Call, req contract.RouterConfigPatch) (contract.RouterSettings, error) {
	cfg, err := s.Tenancy.UpdateRouterConfig(c.Ctx, c.Principal, c.Path("id"), req)
	if err != nil {
		return contract.RouterSettings{}, err
	}
	out, err := s.routerSettings(c, cfg)
	return committed(out, err) // the change is stored: a failure to describe it must still be audited
}

func (s *server) routerStats(c Call, _ none) (contract.RouterStats, error) {
	rr, err := s.routerReader()
	if err != nil {
		return contract.RouterStats{}, err
	}
	r, err := s.savingsRange(c)
	if err != nil {
		return contract.RouterStats{}, err
	}
	cfg, err := s.Tenancy.RouterConfig(c.Ctx, c.Principal, c.Path("id"))
	if err != nil {
		return contract.RouterStats{}, err
	}
	out, err := rr.Stats(c.Ctx, c.Path("id"), r)
	out.Mode = cfg.Mode
	return out, err
}

func (s *server) routerFeedback(c Call, req contract.RouterFeedbackRequest) (contract.RouterFeedbackView, error) {
	rr, err := s.routerReader()
	if err != nil {
		return contract.RouterFeedbackView{}, err
	}
	if !contract.ValidRouterVerdict(req.Verdict) {
		return contract.RouterFeedbackView{}, contract.Invalid("verdict", "must be \"worse\", \"fine\" or null")
	}
	return rr.SetFeedback(c.Ctx, c.Path("id"), c.Path("callId"), c.Principal.UserID, req.Verdict)
}
