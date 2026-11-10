package tenancy

import (
	"context"

	"github.com/ethosagent/meterwise/internal/contract"
)

// MonitorConfig is the workspace's monitor settings with defaults filled in wherever settings.monitors is
// missing, malformed or partial. A workspace outside the org is not_found.
func (s *Service) MonitorConfig(ctx context.Context, p contract.Principal, workspaceID string) (contract.MonitorConfig, error) {
	ws, err := s.st.Workspace(ctx, p.OrgID, workspaceID)
	if err != nil {
		return contract.MonitorConfig{}, err
	}
	return contract.MonitorConfigFrom(ws.Settings["monitors"]), nil
}

// UpdateMonitorConfig merges patch into the stored config under a row lock, validates the result (a 422 whose
// details name the field path, nothing written) and busts the directory cache so the data plane applies the
// new settings at once. Other settings keys are untouched.
func (s *Service) UpdateMonitorConfig(ctx context.Context, p contract.Principal, workspaceID string, patch contract.MonitorConfigPatch) (contract.MonitorConfig, error) {
	cfg, err := s.st.UpdateWorkspaceMonitors(ctx, p.OrgID, workspaceID, func(cur contract.MonitorConfig) (contract.MonitorConfig, error) {
		next := patch.Apply(cur)
		return next, next.Validate()
	})
	if err != nil {
		return contract.MonitorConfig{}, err
	}
	s.dir.Bust(workspaceID)
	return cfg, nil
}
