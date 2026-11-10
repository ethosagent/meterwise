package api

import (
	"time"

	"github.com/ethosagent/meterwise/internal/access"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/tenancy"
)

func (s *server) proxyURL(publicID string) string { return s.proxyPrefix() + publicID }

func (s *server) proxyPrefix() string { return "https://" + s.ProxyPublicHost + "/w/" }

// workspaceViews renders workspaces for the caller. The public id, the proxy URL and the allowed
// networks are the workspace's credential and its guard: they are null unless the caller holds
// workspace_url:read on that workspace, in every response that carries a workspace.
func (s *server) workspaceViews(v *access.GrantView, list []contract.Workspace) ([]contract.WorkspaceView, error) {
	ok, err := allowedWorkspaceIDs(v, contract.PermWorkspaceURLRead, list)
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
	v, err := s.workspaceViews(s.Authz.Scope(c.Ctx, c.Principal), []contract.Workspace{ws})
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
	v := contract.WorkspaceView{ID: ws.ID, Name: ws.Name, IsDefault: ws.IsDefault, Settings: settings, CreatedAt: contract.RFC3339(ws.CreatedAt)}
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

// workspaceSetup renders the setup page. Its row demands workspace_url:read, the same rule that
// gates the URL in workspaceView, so the URL built here is only ever sent to a caller who may see it;
// every URL the API sends goes through proxyURL.
func (s *server) workspaceSetup(c Call, _ none) (contract.Setup, error) {
	st, err := s.Tenancy.Setup(c.Ctx, c.Principal, c.Path("id"))
	if err != nil {
		return contract.Setup{}, err
	}
	url := s.proxyURL(st.PublicID)
	return contract.Setup{ProxyURL: url, Snippets: tenancy.RenderSnippets(url), Status: st.Status}, nil
}

func (s *server) resetWorkspaceURL(c Call, req contract.ResetURLRequest) (contract.ResetURLResponse, error) {
	grace := contract.DefaultGraceMinutes
	if req.GraceMinutes != nil {
		grace = *req.GraceMinutes
	}
	if grace < 0 || grace > contract.MaxGraceMinutes {
		return contract.ResetURLResponse{}, contract.Invalid("graceMinutes", "must be between 0 and 1440")
	}
	id := c.Path("id")
	res, err := s.Tenancy.ResetURL(c.Ctx, c.Principal, id, time.Duration(grace)*time.Minute)
	if err != nil {
		return contract.ResetURLResponse{}, err
	}
	// Managing the URL is not reading it: only a caller who also holds workspace_url:read gets it back.
	v, err := s.workspaceViewOf(c, res.Workspace)
	if err != nil {
		return contract.ResetURLResponse{}, err
	}
	return contract.ResetURLResponse{ProxyURL: v.ProxyURL, PreviousGraceUntil: contract.RFC3339(res.PreviousGraceUntil)}, nil
}

func (s *server) retirePreviousURL(c Call, _ none) (none, error) {
	id := c.Path("id")
	if err := s.Tenancy.RetirePreviousURL(c.Ctx, c.Principal, id); err != nil {
		return none{}, err
	}
	return none{}, nil
}

func (s *server) setAllowedCIDRs(c Call, req contract.AllowedCIDRs) (contract.AllowedCIDRs, error) {
	id := c.Path("id")
	got, err := s.Tenancy.SetAllowedCIDRs(c.Ctx, c.Principal, id, req.CIDRs)
	if err != nil {
		return contract.AllowedCIDRs{}, err
	}
	return contract.AllowedCIDRs{CIDRs: got}, nil
}
