package tenancy

import (
	"context"

	"github.com/ethosagent/meterwise/internal/contract"
)

// The three savers keep their settings under workspaces.settings.{trim,router,memory}, next to monitors. Each
// has a read (defaults filled in wherever the key is missing, malformed or partial) and a patch (merge under a
// row lock, validate, 422 naming the field with nothing written, bust the directory cache so the data plane applies
// the change at once). A workspace outside the org is not_found.

// TrimConfig is the workspace's trim settings.
func (s *Service) TrimConfig(ctx context.Context, p contract.Principal, workspaceID string) (contract.TrimConfig, error) {
	ws, err := s.st.Workspace(ctx, p.OrgID, workspaceID)
	if err != nil {
		return contract.TrimConfig{}, err
	}
	return contract.TrimConfigFrom(ws.Settings[contract.SettingTrim]), nil
}

// UpdateTrimConfig merges patch into the stored trim settings.
func (s *Service) UpdateTrimConfig(ctx context.Context, p contract.Principal, workspaceID string, patch contract.TrimConfigPatch) (contract.TrimConfig, error) {
	var out contract.TrimConfig
	err := s.st.UpdateWorkspaceSetting(ctx, p.OrgID, workspaceID, contract.SettingTrim, func(cur any) (string, error) {
		out = patch.Apply(contract.TrimConfigFrom(cur))
		return out.Attr(), out.Validate()
	})
	if err != nil {
		return contract.TrimConfig{}, err
	}
	s.dir.Bust(workspaceID)
	return out, nil
}

// RouterConfig is the workspace's router settings.
func (s *Service) RouterConfig(ctx context.Context, p contract.Principal, workspaceID string) (contract.RouterConfig, error) {
	ws, err := s.st.Workspace(ctx, p.OrgID, workspaceID)
	if err != nil {
		return contract.RouterConfig{}, err
	}
	return contract.RouterConfigFrom(ws.Settings[contract.SettingRouter]), nil
}

// UpdateRouterConfig merges patch into the stored router settings.
func (s *Service) UpdateRouterConfig(ctx context.Context, p contract.Principal, workspaceID string, patch contract.RouterConfigPatch) (contract.RouterConfig, error) {
	var out contract.RouterConfig
	err := s.st.UpdateWorkspaceSetting(ctx, p.OrgID, workspaceID, contract.SettingRouter, func(cur any) (string, error) {
		out = patch.Apply(contract.RouterConfigFrom(cur))
		return out.Attr(), out.Validate()
	})
	if err != nil {
		return contract.RouterConfig{}, err
	}
	s.dir.Bust(workspaceID)
	return out, nil
}

// MemoryConfig is the workspace's memory settings.
func (s *Service) MemoryConfig(ctx context.Context, p contract.Principal, workspaceID string) (contract.MemoryConfig, error) {
	ws, err := s.st.Workspace(ctx, p.OrgID, workspaceID)
	if err != nil {
		return contract.MemoryConfig{}, err
	}
	return contract.MemoryConfigFrom(ws.Settings[contract.SettingMemory]), nil
}

// UpdateMemoryConfig merges patch into the stored memory settings.
func (s *Service) UpdateMemoryConfig(ctx context.Context, p contract.Principal, workspaceID string, patch contract.MemoryConfigPatch) (contract.MemoryConfig, error) {
	var out contract.MemoryConfig
	err := s.st.UpdateWorkspaceSetting(ctx, p.OrgID, workspaceID, contract.SettingMemory, func(cur any) (string, error) {
		out = patch.Apply(contract.MemoryConfigFrom(cur))
		return out.Attr(), out.Validate()
	})
	if err != nil {
		return contract.MemoryConfig{}, err
	}
	s.dir.Bust(workspaceID)
	return out, nil
}
