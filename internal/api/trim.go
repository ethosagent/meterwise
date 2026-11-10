package api

import (
	"context"

	"github.com/ethosagent/meterwise/internal/contract"
)

// TrimReader is what the control plane reads about trimming; the trim feature's store implements it and cmd wires it.
// Both reads are scoped by the workspace id the gateway authorized. A nil port answers with not_available. The
// handler sets TrimStats.Mode from the workspace's settings, so the port leaves it empty.
type TrimReader interface {
	Stats(ctx context.Context, workspaceID string, r contract.SavingsRange) (contract.TrimStats, error)
	// Preview reads what trimming did to one call (sizes and markers only); an unknown call is not_found.
	Preview(ctx context.Context, workspaceID, callID string) (contract.TrimPreview, error)
}

// registerTrim wires the trim routes. The gateway has authorized the workspace and the permission.
func (s *server) registerTrim() {
	register(s, "GET", "/api/workspaces/{id}/trim", s.getTrim)
	register(s, "PATCH", "/api/workspaces/{id}/trim", s.patchTrim)
	register(s, "GET", "/api/workspaces/{id}/trim/stats", s.trimStats)
	register(s, "GET", "/api/workspaces/{id}/calls/{callId}/trim", s.trimPreview)
}

func (s *server) trimReader() (TrimReader, error) {
	if s.Trim == nil {
		return nil, contract.Unavailable("trim", "")
	}
	return s.Trim, nil
}

func (s *server) getTrim(c Call, _ none) (contract.TrimConfig, error) {
	return s.Tenancy.TrimConfig(c.Ctx, c.Principal, c.Path("id"))
}

func (s *server) patchTrim(c Call, req contract.TrimConfigPatch) (contract.TrimConfig, error) {
	return s.Tenancy.UpdateTrimConfig(c.Ctx, c.Principal, c.Path("id"), req)
}

func (s *server) trimStats(c Call, _ none) (contract.TrimStats, error) {
	tr, err := s.trimReader()
	if err != nil {
		return contract.TrimStats{}, err
	}
	r, err := s.savingsRange(c)
	if err != nil {
		return contract.TrimStats{}, err
	}
	cfg, err := s.Tenancy.TrimConfig(c.Ctx, c.Principal, c.Path("id"))
	if err != nil {
		return contract.TrimStats{}, err
	}
	out, err := tr.Stats(c.Ctx, c.Path("id"), r)
	out.Mode = cfg.Mode
	return out, err
}

func (s *server) trimPreview(c Call, _ none) (contract.TrimPreview, error) {
	tr, err := s.trimReader()
	if err != nil {
		return contract.TrimPreview{}, err
	}
	return tr.Preview(c.Ctx, c.Path("id"), c.Path("callId"))
}
