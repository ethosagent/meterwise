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
)

// Tenancy is what the handlers need from the tenancy layer; *tenancy.Service
// implements it. Every method that touches org data takes the calling principal.
type Tenancy interface {
	CompleteSignup(ctx context.Context, sess contract.Session) (contract.Session, error)
	Profile(ctx context.Context, userID string) (contract.Preferences, string, error)
	Preferences(ctx context.Context, userID string) (contract.Preferences, error)
	UpdatePreferences(ctx context.Context, userID string, patch contract.PreferencesPatch) (contract.Preferences, error)

	ListWorkspaces(ctx context.Context, p contract.Principal) ([]contract.Workspace, error)
	CreateWorkspace(ctx context.Context, p contract.Principal, name string) (contract.Workspace, error)
	UpdateWorkspace(ctx context.Context, p contract.Principal, id string, req contract.UpdateWorkspaceRequest) (contract.Workspace, error)
	DeleteWorkspace(ctx context.Context, p contract.Principal, id string) error
	SwitchWorkspace(ctx context.Context, p contract.Principal, id string) (string, error)

	ListInvitations(ctx context.Context, p contract.Principal) ([]contract.Invitation, error)
	CreateInvitation(ctx context.Context, p contract.Principal, email string, role contract.Role) (contract.Invitation, error)
	ResendInvitation(ctx context.Context, p contract.Principal, id string) (contract.Invitation, error)
	RevokeInvitation(ctx context.Context, p contract.Principal, id string) error

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

func (s *server) workspaceView(ws contract.Workspace) contract.WorkspaceView {
	settings := ws.Settings
	if settings == nil {
		settings = map[string]any{}
	}
	return contract.WorkspaceView{
		ID: ws.ID, PublicID: ws.PublicID, Name: ws.Name, IsDefault: ws.IsDefault,
		ProxyURL: "https://" + s.ProxyPublicHost + "/w/" + ws.PublicID, Settings: settings, CreatedAt: rfc3339(ws.CreatedAt),
	}
}

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

// me builds the Me body for a session that holds a membership.
func (s *server) me(ctx context.Context, sess contract.Session) (contract.Me, error) {
	prefs, active, err := s.Tenancy.Profile(ctx, sess.User.ID)
	if err != nil {
		return contract.Me{}, err
	}
	return contract.Me{
		User:  contract.MeUser{ID: sess.User.ID, Email: sess.User.Email, Name: sess.User.Name, AvatarURL: sess.User.AvatarURL},
		OrgID: sess.Principal.OrgID, Role: sess.Principal.Role, ActiveWorkspaceID: active, Preferences: prefs,
	}, nil
}

func (s *server) getMe(w http.ResponseWriter, r *http.Request) {
	sess, _ := SessionFrom(r.Context())
	if !sess.Member {
		writeError(w, contract.NewError(contract.CodeNotInvited, "this account has not been invited to the organization"))
		return
	}
	me, err := s.me(r.Context(), sess)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, me)
}

func (s *server) completeSignup(w http.ResponseWriter, r *http.Request) {
	sess, _ := SessionFrom(r.Context())
	sess, err := s.Tenancy.CompleteSignup(r.Context(), sess)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	me, err := s.me(r.Context(), sess)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, me)
}

func (s *server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	list, err := s.Tenancy.ListWorkspaces(r.Context(), p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := contract.WorkspaceList{Items: make([]contract.WorkspaceView, 0, len(list))}
	for _, ws := range list {
		out.Items = append(out.Items, s.workspaceView(ws))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	var req contract.CreateWorkspaceRequest
	if !readJSON(w, r, &req) {
		return
	}
	ws, err := s.Tenancy.CreateWorkspace(r.Context(), p, req.Name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.workspaceView(ws))
}

func (s *server) updateWorkspace(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	var req contract.UpdateWorkspaceRequest
	if !readJSON(w, r, &req) {
		return
	}
	ws, err := s.Tenancy.UpdateWorkspace(r.Context(), p, r.PathValue("id"), req)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.workspaceView(ws))
}

func (s *server) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	if err := s.Tenancy.DeleteWorkspace(r.Context(), p, r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) switchWorkspace(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id, err := s.Tenancy.SwitchWorkspace(r.Context(), p, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, contract.SwitchWorkspaceResponse{ActiveWorkspaceID: id})
}

func (s *server) listInvitations(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	list, err := s.Tenancy.ListInvitations(r.Context(), p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	now := time.Now()
	out := contract.InvitationList{Items: make([]contract.InvitationView, 0, len(list))}
	for _, i := range list {
		out.Items = append(out.Items, invitationView(i, now))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) createInvitation(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	var req contract.CreateInvitationRequest
	if !readJSON(w, r, &req) {
		return
	}
	inv, err := s.Tenancy.CreateInvitation(r.Context(), p, req.Email, req.Role)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, invitationView(inv, time.Now()))
}

func (s *server) resendInvitation(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	inv, err := s.Tenancy.ResendInvitation(r.Context(), p, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, invitationView(inv, time.Now()))
}

func (s *server) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	if err := s.Tenancy.RevokeInvitation(r.Context(), p, r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
