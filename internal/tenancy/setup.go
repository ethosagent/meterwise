package tenancy

import (
	"context"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Setup returns what the setup page needs besides the URL: the workspace's current public id and the
// connection status. The API builds the URL from the id through its one builder and renders the
// snippets with RenderSnippets. Status comes from the ledger: firstCallAt is the earliest call,
// lastCallAt is workspaces.last_call_at (the coalesced touch the ledger writes), callsToday counts
// the UTC day.
func (s *Service) Setup(ctx context.Context, p contract.Principal, workspaceID string) (contract.SetupState, error) {
	ws, err := s.st.Workspace(ctx, p.OrgID, workspaceID)
	if err != nil {
		return contract.SetupState{}, err
	}
	st := contract.SetupStatus{}
	if ws.LastCallAt != nil {
		last := contract.RFC3339(*ws.LastCallAt)
		st.LastCallAt = &last
	}
	cs, err := s.st.CallStatus(ctx, ws.ID, s.cfg.Now().UTC().Truncate(24*time.Hour))
	if err != nil {
		return contract.SetupState{}, err
	}
	if cs.FirstCallAt != nil {
		first := contract.RFC3339(*cs.FirstCallAt)
		st.FirstCallAt = &first
	}
	st.CallsToday = cs.CallsToday
	return contract.SetupState{PublicID: ws.PublicID, Status: st}, nil
}

// SetAllowedCIDRs replaces the workspace's allowed source networks ([] clears) and busts the
// directory cache so the data plane sees the change at once.
func (s *Service) SetAllowedCIDRs(ctx context.Context, p contract.Principal, workspaceID string, cidrs []string) ([]string, error) {
	norm, err := NormalizeCIDRs(cidrs)
	if err != nil {
		return nil, err
	}
	out, err := s.st.SetAllowedClientCIDRs(ctx, p.OrgID, workspaceID, norm)
	if err != nil {
		return nil, err
	}
	s.dir.Bust(workspaceID)
	return out, nil
}

// ResetURL rotates the workspace's public id; the old one keeps answering for grace.
func (s *Service) ResetURL(ctx context.Context, p contract.Principal, workspaceID string, grace time.Duration) (contract.URLReset, error) {
	return s.dir.Reset(ctx, p, workspaceID, grace)
}

// RetirePreviousURL ends the grace of the workspace's retired ids at once.
func (s *Service) RetirePreviousURL(ctx context.Context, p contract.Principal, workspaceID string) error {
	return s.dir.RetirePrevious(ctx, p, workspaceID)
}
