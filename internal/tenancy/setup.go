package tenancy

import (
	"context"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Setup returns what the setup page shows: the URL, the snippets rendered for it and the status.
// proxyPrefix is "https://<host>/w/" (the caller owns the public host); the workspace's current
// public id is appended to it. Status reads workspaces.last_call_at;
// the first-call time and the daily count stay null/0 until the ledger records calls (Plan 05).
func (s *Service) Setup(ctx context.Context, p contract.Principal, workspaceID, proxyPrefix string) (contract.Setup, error) {
	ws, err := s.st.Workspace(ctx, p.OrgID, workspaceID)
	if err != nil {
		return contract.Setup{}, err
	}
	proxyURL := proxyPrefix + ws.PublicID
	st := contract.SetupStatus{}
	if ws.LastCallAt != nil {
		last := ws.LastCallAt.UTC().Format(time.RFC3339)
		st.LastCallAt = &last
	}
	return contract.Setup{ProxyURL: proxyURL, Snippets: SnippetRenderer{}.Render(proxyURL), Status: st}, nil
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
