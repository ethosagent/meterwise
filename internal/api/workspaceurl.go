package api

import (
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

const (
	defaultGraceMinutes = 10
	maxGraceMinutes     = 24 * 60
)

func (s *server) proxyURL(publicID string) string { return s.proxyPrefix() + publicID }

func (s *server) proxyPrefix() string { return "https://" + s.ProxyPublicHost + "/w/" }

// workspaceViews renders workspaces for the caller. The public id, the proxy URL and the allowed
// networks are the workspace's credential and its guard: they are null unless the caller holds
// workspace_url:read on that workspace, in every response that carries a workspace.
func (s *server) workspaceViews(c Call, list []contract.Workspace) ([]contract.WorkspaceView, error) {
	ids := make([]string, len(list))
	for i, ws := range list {
		ids[i] = ws.ID
	}
	ok, err := s.Authz.Filter(c.Ctx, c.Principal, contract.PermWorkspaceURLRead, ids)
	if err != nil {
		return nil, err
	}
	may := make(map[string]bool, len(ok))
	for _, id := range ok {
		may[id] = true
	}
	out := make([]contract.WorkspaceView, len(list))
	for i, ws := range list {
		out[i] = s.workspaceView(ws, may[ws.ID])
	}
	return out, nil
}

func (s *server) workspaceViewOf(c Call, ws contract.Workspace) (contract.WorkspaceView, error) {
	v, err := s.workspaceViews(c, []contract.Workspace{ws})
	if err != nil {
		return contract.WorkspaceView{}, err
	}
	return v[0], nil
}

func (s *server) workspaceView(ws contract.Workspace, withURL bool) contract.WorkspaceView {
	settings := ws.Settings
	if settings == nil {
		settings = map[string]any{}
	}
	v := contract.WorkspaceView{ID: ws.ID, Name: ws.Name, IsDefault: ws.IsDefault, Settings: settings, CreatedAt: rfc3339(ws.CreatedAt)}
	if withURL {
		pid, url := ws.PublicID, s.proxyURL(ws.PublicID)
		cidrs := ws.AllowedClientCIDRs
		if cidrs == nil {
			cidrs = []string{}
		}
		v.PublicID, v.ProxyURL, v.AllowedClientCidrs = &pid, &url, &cidrs
	}
	return v
}

func (s *server) workspaceSetup(c Call, _ none) (contract.Setup, error) {
	return s.Tenancy.Setup(c.Ctx, c.Principal, c.Path("id"), s.proxyPrefix())
}

func (s *server) resetWorkspaceURL(c Call, req contract.ResetURLRequest) (contract.ResetURLResponse, error) {
	grace := defaultGraceMinutes
	if req.GraceMinutes != nil {
		grace = *req.GraceMinutes
	}
	if grace < 0 || grace > maxGraceMinutes {
		return contract.ResetURLResponse{}, contract.Invalid("graceMinutes", "must be between 0 and 1440")
	}
	id := c.Path("id")
	res, err := s.Tenancy.ResetURL(c.Ctx, c.Principal, id, time.Duration(grace)*time.Minute)
	if err != nil {
		return contract.ResetURLResponse{}, err
	}
	s.audit(c, contract.AuditWorkspaceURLReset, "workspace", id, id, map[string]any{"graceMinutes": grace})
	return contract.ResetURLResponse{ProxyURL: s.proxyURL(res.Workspace.PublicID), PreviousGraceUntil: rfc3339(res.PreviousGraceUntil)}, nil
}

func (s *server) retirePreviousURL(c Call, _ none) (none, error) {
	id := c.Path("id")
	if err := s.Tenancy.RetirePreviousURL(c.Ctx, c.Principal, id); err != nil {
		return none{}, err
	}
	s.audit(c, contract.AuditWorkspaceURLRetire, "workspace", id, id, nil)
	return none{}, nil
}

func (s *server) setAllowedCIDRs(c Call, req contract.AllowedCIDRs) (contract.AllowedCIDRs, error) {
	id := c.Path("id")
	got, err := s.Tenancy.SetAllowedCIDRs(c.Ctx, c.Principal, id, req.CIDRs)
	if err != nil {
		return contract.AllowedCIDRs{}, err
	}
	s.audit(c, contract.AuditWorkspaceAllowedCIDRs, "workspace", id, id, map[string]any{"count": len(got)})
	return contract.AllowedCIDRs{CIDRs: got}, nil
}
