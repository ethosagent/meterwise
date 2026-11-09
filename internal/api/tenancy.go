package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/identity"
	"github.com/ethosagent/meterwise/internal/tenancy"
)

// Tenancy is what the handlers need from the tenancy layer; *tenancy.Service
// implements it. Every method that touches org data takes the calling principal.
type Tenancy interface {
	CompleteSignup(ctx context.Context, sess contract.Session) (contract.Session, error)
	Profile(ctx context.Context, userID string) (contract.Profile, error)
	UpdatePreferences(ctx context.Context, userID string, patch contract.PreferencesPatch) (contract.Preferences, error)

	ListWorkspaces(ctx context.Context, p contract.Principal) ([]contract.Workspace, error)
	CreateWorkspace(ctx context.Context, p contract.Principal, name string) (contract.Workspace, error)
	UpdateWorkspace(ctx context.Context, p contract.Principal, id string, req contract.UpdateWorkspaceRequest) (contract.Workspace, error)
	DeleteWorkspace(ctx context.Context, p contract.Principal, id string) error
	SwitchWorkspace(ctx context.Context, p contract.Principal, id string) (string, error)
	Setup(ctx context.Context, p contract.Principal, workspaceID, proxyPrefix string) (contract.Setup, error)
	ResetURL(ctx context.Context, p contract.Principal, workspaceID string, grace time.Duration) (contract.URLReset, error)
	RetirePreviousURL(ctx context.Context, p contract.Principal, workspaceID string) error
	SetAllowedCIDRs(ctx context.Context, p contract.Principal, workspaceID string, cidrs []string) ([]string, error)

	ListInvitations(ctx context.Context, p contract.Principal) ([]contract.Invitation, error)
	CreateInvitation(ctx context.Context, p contract.Principal, email string, role contract.Role) (contract.Invitation, error)
	ResendInvitation(ctx context.Context, p contract.Principal, id string) (contract.Invitation, error)
	RevokeInvitation(ctx context.Context, p contract.Principal, id string) error

	ListMembers(ctx context.Context, p contract.Principal) ([]contract.Member, error)
	SetMemberRole(ctx context.Context, p contract.Principal, userID string, role contract.Role) (contract.Member, error)
	RemoveMember(ctx context.Context, p contract.Principal, userID string) error

	ApplyWebhookUser(ctx context.Context, u identity.WebhookUser) error
	DeleteWebhookUser(ctx context.Context, clerkUserID string) error
}

// WebhookVerifier authenticates a Clerk webhook (the Svix signature).
type WebhookVerifier interface {
	Verify(payload []byte, h http.Header) (identity.WebhookEvent, error)
	MarkProcessed(id string)
}

const maxWebhookBytes = 256 << 10

// readJSON decodes one JSON object into dst; a malformed body is a 422 validation_failed.
func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, contract.NewError(contract.CodeValidationFailed, "request validation failed", "body: "+decodeProblem(err)))
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writeError(w, contract.NewError(contract.CodeValidationFailed, "request validation failed", "body: must be a single JSON object"))
		return false
	}
	return true
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func invitationView(i contract.Invitation, now time.Time) contract.InvitationView {
	by := i.InvitedBy
	if by == "" {
		by = "meterwised"
	}
	return contract.InvitationView{
		ID: i.ID, Email: i.Email, Role: i.Role, Status: i.StatusAt(now), InvitedBy: by,
		ExpiresAt: rfc3339(i.ExpiresAt), CreatedAt: rfc3339(i.CreatedAt),
	}
}

// me builds the Me body for a session that holds a membership. The active workspace is the stored
// one when the caller can still read it, otherwise the first workspace they can read (default
// first); null when they can read none.
func (s *server) me(c Call) (contract.Me, error) {
	prof, err := s.Tenancy.Profile(c.Ctx, c.Session.User.ID)
	if err != nil {
		return contract.Me{}, err
	}
	list, err := s.readableWorkspaces(c)
	if err != nil {
		return contract.Me{}, err
	}
	var active *string
	if id := tenancy.ActiveWorkspace(prof.ActiveWorkspaceID, list); id != "" {
		active = &id
	}
	u := c.Session.User
	return contract.Me{
		User:  contract.MeUser{ID: u.ID, Email: u.Email, Name: u.Name, AvatarURL: u.AvatarURL},
		OrgID: c.Session.Principal.OrgID, Role: c.Session.Principal.Role, ActiveWorkspaceID: active, Preferences: prof.Preferences,
	}, nil
}

// readableWorkspaces lists the org's workspaces the caller holds workspace:read on, default first.
func (s *server) readableWorkspaces(c Call) ([]contract.Workspace, error) {
	all, err := s.Tenancy.ListWorkspaces(c.Ctx, c.Principal)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(all))
	for i, w := range all {
		ids[i] = w.ID
	}
	ok, err := s.Authz.Filter(c.Ctx, c.Principal, contract.PermWorkspaceRead, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]contract.Workspace, len(all))
	for _, w := range all {
		byID[w.ID] = w
	}
	out := make([]contract.Workspace, 0, len(ok))
	for _, id := range ok { // Filter keeps the input order, default first
		out = append(out, byID[id])
	}
	return out, nil
}

func (s *server) getMe(c Call, _ none) (contract.Me, error) {
	if !c.Session.Member {
		return contract.Me{}, contract.NotInvited()
	}
	return s.me(c)
}

func (s *server) completeSignup(c Call, _ none) (contract.Me, error) {
	sess, err := s.Tenancy.CompleteSignup(c.Ctx, c.Session)
	if err != nil {
		return contract.Me{}, err
	}
	c.Session, c.Principal = sess, sess.Principal
	return s.me(c)
}

func (s *server) mePermissions(c Call, _ none) (contract.PermissionSnapshot, error) {
	if !c.Session.Member {
		return contract.PermissionSnapshot{}, contract.NotInvited()
	}
	return s.Authz.Snapshot(c.Ctx, c.Principal)
}

func (s *server) listWorkspaces(c Call, _ none) (contract.WorkspaceList, error) {
	list, err := s.readableWorkspaces(c)
	if err != nil {
		return contract.WorkspaceList{}, err
	}
	views, err := s.workspaceViews(c, list)
	return contract.WorkspaceList{Items: views}, err
}

func (s *server) createWorkspace(c Call, req contract.CreateWorkspaceRequest) (contract.WorkspaceView, error) {
	ws, err := s.Tenancy.CreateWorkspace(c.Ctx, c.Principal, req.Name)
	if err != nil {
		return contract.WorkspaceView{}, err
	}
	return s.workspaceViewOf(c, ws)
}

func (s *server) updateWorkspace(c Call, req contract.UpdateWorkspaceRequest) (contract.WorkspaceView, error) {
	ws, err := s.Tenancy.UpdateWorkspace(c.Ctx, c.Principal, c.Path("id"), req)
	if err != nil {
		return contract.WorkspaceView{}, err
	}
	return s.workspaceViewOf(c, ws)
}

func (s *server) deleteWorkspace(c Call, _ none) (none, error) {
	id := c.Path("id")
	if err := s.Tenancy.DeleteWorkspace(c.Ctx, c.Principal, id); err != nil {
		return none{}, err
	}
	return none{}, nil
}

func (s *server) switchWorkspace(c Call, _ none) (contract.SwitchWorkspaceResponse, error) {
	id, err := s.Tenancy.SwitchWorkspace(c.Ctx, c.Principal, c.Path("id"))
	return contract.SwitchWorkspaceResponse{ActiveWorkspaceID: id}, err
}

func (s *server) listMembers(c Call, _ none) (contract.MemberList, error) {
	list, err := s.Tenancy.ListMembers(c.Ctx, c.Principal)
	return contract.MemberList{Items: list}, err
}

func (s *server) updateMember(c Call, req contract.UpdateMemberRequest) (contract.Member, error) {
	m, err := s.Tenancy.SetMemberRole(c.Ctx, c.Principal, c.Path("userId"), req.Role)
	return m, err
}

func (s *server) removeMember(c Call, _ none) (none, error) {
	if err := s.Tenancy.RemoveMember(c.Ctx, c.Principal, c.Path("userId")); err != nil {
		return none{}, err
	}
	return none{}, nil
}

func (s *server) listInvitations(c Call, _ none) (contract.InvitationList, error) {
	list, err := s.Tenancy.ListInvitations(c.Ctx, c.Principal)
	if err != nil {
		return contract.InvitationList{}, err
	}
	now := time.Now()
	out := contract.InvitationList{Items: make([]contract.InvitationView, 0, len(list))}
	for _, i := range list {
		out.Items = append(out.Items, invitationView(i, now))
	}
	return out, nil
}

func (s *server) createInvitation(c Call, req contract.CreateInvitationRequest) (contract.InvitationView, error) {
	inv, err := s.Tenancy.CreateInvitation(c.Ctx, c.Principal, req.Email, req.Role)
	if err != nil {
		return contract.InvitationView{}, err
	}
	return invitationView(inv, time.Now()), nil
}

func (s *server) resendInvitation(c Call, _ none) (contract.InvitationView, error) {
	inv, err := s.Tenancy.ResendInvitation(c.Ctx, c.Principal, c.Path("id"))
	if err != nil {
		return contract.InvitationView{}, err
	}
	return invitationView(inv, time.Now()), nil
}

func (s *server) revokeInvitation(c Call, _ none) (none, error) {
	if err := s.Tenancy.RevokeInvitation(c.Ctx, c.Principal, c.Path("id")); err != nil {
		return none{}, err
	}
	return none{}, nil
}

// clerkWebhook authenticates by Svix signature, not by bearer token. A bad
// signature, a stale timestamp and a replay all answer the same 400.
func (s *server) clerkWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Webhooks == nil {
		writeError(w, contract.NewError(contract.CodeNotAvailable, "the Clerk webhook is not configured"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
	if err != nil {
		writeError(w, contract.NewError(contract.CodeInvalidRequest, "webhook rejected"))
		return
	}
	ev, err := s.Webhooks.Verify(body, r.Header)
	if err != nil {
		writeError(w, contract.NewError(contract.CodeInvalidRequest, "webhook rejected"))
		return
	}
	switch ev.Type {
	case "user.created", "user.updated":
		err = s.Tenancy.ApplyWebhookUser(r.Context(), ev.User)
	case "user.deleted":
		err = s.Tenancy.DeleteWebhookUser(r.Context(), ev.User.ClerkUserID)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.Webhooks.MarkProcessed(ev.ID)
	w.WriteHeader(http.StatusNoContent)
}
