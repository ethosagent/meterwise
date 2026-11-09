package tenancy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/ethosagent/meterwise/internal/access"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/identity"
	"github.com/ethosagent/meterwise/internal/store"
)

const (
	maxIDAttempts = 5
	// DefaultWorkspaceName names the workspace created at first boot.
	DefaultWorkspaceName = "Default"
)

// Config tunes the service.
type Config struct {
	MaxWorkspaces int
	Now           func() time.Time // default time.Now
	Rand          io.Reader        // default crypto/rand
	Log           *slog.Logger     // best-effort provider cleanups are logged here; default discards
}

// Service is the tenancy use-case layer: it resolves sessions, completes sign-up
// from an invitation and manages workspaces, invitations and preferences.
type Service struct {
	st  *store.Store
	idp contract.IdentityProvider
	dir *Directory
	cfg Config
	org string
}

var _ contract.PrincipalResolver = (*Service)(nil)

// New builds the service. idp may be nil only if invitations are never used.
func New(st *store.Store, idp contract.IdentityProvider, cfg Config) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MaxWorkspaces < 1 {
		cfg.MaxWorkspaces = 1
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Service{st: st, idp: idp, dir: NewDirectory(st, cfg.Now), cfg: cfg, org: store.OrgID()}
}

// Directory returns the cached workspace directory.
func (s *Service) Directory() *Directory { return s.dir }

// inviteEmail normalises an address typed by a person: surrounding ASCII whitespace is dropped (the
// one place anything is trimmed), then contract.NormalizeEmail judges the rest.
func inviteEmail(raw string) (string, error) {
	if e, ok := contract.NormalizeEmail(strings.Trim(raw, " \t\r\n")); ok {
		return e, nil
	}
	return "", contract.Invalid("email", "must be a plain ASCII email address")
}

// Resolve mirrors the identity into users (the lazy upsert: it works with zero
// webhooks) and attaches the principal for an active membership. A soft-deleted
// user is unauthenticated. Membership is read on every call, so removing it
// takes effect on the next request. The common case is one joined read.
func (s *Service) Resolve(ctx context.Context, id contract.Identity) (contract.Session, error) {
	u, role, active, err := s.st.ResolveUser(ctx, s.org, id)
	if err != nil {
		return contract.Session{}, err
	}
	if u.Deleted {
		return contract.Session{}, contract.NewError(contract.CodeUnauthenticated, "invalid or expired token")
	}
	sess := contract.Session{Identity: id, User: u}
	if active {
		sess = s.asMember(sess, role)
	}
	return sess, nil
}

// asMember marks the session's user as an active member with role.
func (s *Service) asMember(sess contract.Session, role contract.Role) contract.Session {
	sess.Member = true
	sess.Principal = contract.Principal{UserID: sess.User.ID, OrgID: s.org, Email: sess.User.Email, Role: role}
	return sess
}

// CompleteSignup turns the pending invitation for the session's verified email
// into an active membership. An existing member is returned unchanged. Anyone
// else, including a user whose email is unverified, is not_invited.
func (s *Service) CompleteSignup(ctx context.Context, sess contract.Session) (contract.Session, error) {
	if sess.Member {
		return sess, nil
	}
	if !sess.Identity.EmailVerified {
		return contract.Session{}, contract.NotInvited()
	}
	email, ok := contract.NormalizeEmail(sess.Identity.Email)
	if !ok {
		return contract.Session{}, contract.NotInvited()
	}
	role, ok, err := s.st.AcceptOpenInvitation(ctx, s.org, sess.User.ID, email, s.cfg.Now())
	if err != nil {
		return contract.Session{}, err
	}
	if ok {
		return s.asMember(sess, role), nil
	}
	// Two tabs of the same person finish first login together: the loser finds the invitation
	// already used, but the person is a member by then. Re-read before refusing; a failed
	// re-read is a failure of ours, never a verdict on the person.
	r, active, _, err := s.st.Membership(ctx, s.org, sess.User.ID)
	if err != nil {
		return contract.Session{}, err
	}
	if !active {
		return contract.Session{}, contract.NotInvited()
	}
	return s.asMember(sess, r), nil
}

// EnsureMember makes a user an active member with role (the dev-only resolver and tests use it;
// production membership comes from invitations).
func (s *Service) EnsureMember(ctx context.Context, userID string, role contract.Role) error {
	return s.st.UpsertActiveMembership(ctx, s.org, userID, role)
}

// ApplyWebhookUser upserts a user from a Clerk webhook.
func (s *Service) ApplyWebhookUser(ctx context.Context, u identity.WebhookUser) error {
	email, verified := u.Email, u.EmailVerified
	if _, ok := contract.NormalizeEmail(email); !ok {
		email, verified = "", false // never trimmed or folded into a match, as for token claims
	}
	_, err := s.st.UpsertUser(ctx, s.org, contract.Identity{Subject: u.ClerkUserID, Email: email, EmailVerified: verified, Name: u.Name, AvatarURL: u.AvatarURL})
	return err
}

// DeleteWebhookUser soft-deletes a user and removes their memberships.
func (s *Service) DeleteWebhookUser(ctx context.Context, clerkUserID string) error {
	return s.st.SoftDeleteUser(ctx, s.org, clerkUserID)
}

// Profile returns the caller's stored preferences and stored active workspace id.
// ActiveWorkspace turns the latter into the workspace the caller really uses.
func (s *Service) Profile(ctx context.Context, userID string) (contract.Profile, error) {
	return s.st.Profile(ctx, s.org, userID)
}

// ActiveWorkspace picks the caller's active workspace: the stored one when list (the workspaces
// the caller can read, default first) still holds it, otherwise the first of list. "" means the
// caller can read none.
func ActiveWorkspace(stored string, list []contract.Workspace) string {
	for _, w := range list {
		if w.ID == stored {
			return stored
		}
	}
	if len(list) == 0 {
		return ""
	}
	return list[0].ID
}

// UpdatePreferences applies the fields present in patch.
func (s *Service) UpdatePreferences(ctx context.Context, userID string, patch contract.PreferencesPatch) (contract.Preferences, error) {
	m := map[string]any{}
	if patch.Theme != nil {
		m["theme"] = string(*patch.Theme)
	}
	if patch.ReducedMotion != nil {
		m["reducedMotion"] = *patch.ReducedMotion
	}
	p, err := s.st.MergeProfile(ctx, s.org, userID, m)
	return p.Preferences, err
}

// ListWorkspaces returns the org's workspaces.
func (s *Service) ListWorkspaces(ctx context.Context, p contract.Principal) ([]contract.Workspace, error) {
	return s.dir.List(ctx, p)
}

// withPublicID draws a fresh CSPRNG public id and calls try with it, drawing again
// when try reports a collision (store.ErrPublicIDTaken).
func (s *Service) withPublicID(try func(publicID string) error) error {
	for range maxIDAttempts {
		pid, err := NewPublicID(s.cfg.Rand)
		if err != nil {
			return err
		}
		if err := try(pid); !errors.Is(err, store.ErrPublicIDTaken) {
			return err
		}
	}
	return contract.NewError(contract.CodeInternal, "could not allocate a workspace id")
}

// CreateWorkspace creates a workspace with a fresh CSPRNG public id, honouring MAX_WORKSPACES.
func (s *Service) CreateWorkspace(ctx context.Context, p contract.Principal, name string) (ws contract.Workspace, err error) {
	n, err := contract.ValidName(name)
	if err != nil {
		return contract.Workspace{}, err
	}
	err = s.withPublicID(func(pid string) error {
		var cerr error
		ws, cerr = s.st.CreateWorkspace(ctx, p.OrgID, n, pid, s.cfg.MaxWorkspaces)
		return cerr
	})
	return ws, err
}

// EnsureDefaults creates the default workspace when the org has none. It is idempotent.
func (s *Service) EnsureDefaults(ctx context.Context) error {
	return s.withPublicID(func(pid string) error {
		_, err := s.st.EnsureDefaultWorkspace(ctx, s.org, DefaultWorkspaceName, pid)
		return err
	})
}

// UpdateWorkspace renames a workspace and/or replaces its settings, then busts the directory cache.
func (s *Service) UpdateWorkspace(ctx context.Context, p contract.Principal, id string, req contract.UpdateWorkspaceRequest) (contract.Workspace, error) {
	var name *string
	if req.Name != nil {
		n, err := contract.ValidName(*req.Name)
		if err != nil {
			return contract.Workspace{}, err
		}
		name = &n
	}
	ws, err := s.st.UpdateWorkspace(ctx, p.OrgID, id, name, req.Settings)
	if err == nil {
		s.dir.Bust(ws.ID)
	}
	return ws, err
}

// DeleteWorkspace deletes a non-default workspace and busts the directory cache.
func (s *Service) DeleteWorkspace(ctx context.Context, p contract.Principal, id string) error {
	if err := s.st.DeleteWorkspace(ctx, p.OrgID, id); err != nil {
		return err
	}
	s.dir.Bust(id)
	return nil
}

// SwitchWorkspace stores the caller's active workspace; a workspace outside the org is not_found.
func (s *Service) SwitchWorkspace(ctx context.Context, p contract.Principal, id string) (string, error) {
	ws, err := s.st.Workspace(ctx, p.OrgID, id)
	if err != nil {
		return "", err
	}
	if _, err := s.st.MergeProfile(ctx, p.OrgID, p.UserID, map[string]any{"activeWorkspaceId": ws.ID}); err != nil {
		return "", err
	}
	return ws.ID, nil
}

// ListInvitations returns the org's invitations.
func (s *Service) ListInvitations(ctx context.Context, p contract.Principal) ([]contract.Invitation, error) {
	return s.st.ListInvitations(ctx, p.OrgID)
}

// provider invitations are optional only when the provider is not configured
// (dev); a provider that is configured and fails is an error.
func (s *Service) providerInvite(ctx context.Context, email string) (string, error) {
	id, err := s.idp.CreateInvitation(ctx, email)
	if contract.AsError(err).Code == contract.CodeNotAvailable {
		return "", nil
	}
	return id, err
}

func (s *Service) invite(ctx context.Context, byUserID, rawEmail string, role contract.Role) (contract.Invitation, error) {
	email, err := inviteEmail(rawEmail)
	if err != nil {
		return contract.Invitation{}, err
	}
	if !role.Valid() {
		return contract.Invitation{}, contract.Invalid("role", "must be admin or member")
	}
	if member, err := s.st.ActiveMemberWithEmail(ctx, s.org, email); err != nil {
		return contract.Invitation{}, err
	} else if member {
		return contract.Invitation{}, contract.NewError(contract.CodeAlreadyMember, "that email already belongs to an active member")
	}
	now := s.cfg.Now()
	if _, err := s.st.OpenInvitation(ctx, s.org, email, now); err == nil {
		return contract.Invitation{}, contract.NewError(contract.CodeDuplicateInvitation, "an invitation for that email is already pending")
	}
	clerkID, err := s.providerInvite(ctx, email)
	if err != nil {
		return contract.Invitation{}, err
	}
	inv, err := s.st.CreateInvitation(ctx, store.NewInvitation{
		OrgID: s.org, Email: email, Role: role, ClerkInvitationID: clerkID, InvitedBy: byUserID,
		ExpiresAt: now.Add(identity.InvitationLifetime), Now: now,
	})
	if err != nil {
		s.compensate(ctx, "invite: database write failed", clerkID) // do not leave an orphan that emails the person
		return contract.Invitation{}, err
	}
	return inv, nil
}

// CreateInvitation invites an email as the caller. Only an admin may invite an admin
// (access.MayAssignRole); the route's member:manage grant alone is not enough for that.
func (s *Service) CreateInvitation(ctx context.Context, p contract.Principal, email string, role contract.Role) (contract.Invitation, error) {
	if err := access.MayAssignRole(p.Role, role); err != nil {
		return contract.Invitation{}, err
	}
	return s.invite(ctx, p.UserID, email, role)
}

// BootstrapAdmin is the operator's admin invitation, for when no admin is active (first install,
// or the last admin was deleted at the provider). While any admin is active it refuses with a
// conflict. It is idempotent: an open admin invitation for the email is returned with created=false.
func (s *Service) BootstrapAdmin(ctx context.Context, email string) (inv contract.Invitation, created bool, err error) {
	e, err := inviteEmail(email)
	if err != nil {
		return contract.Invitation{}, false, err
	}
	// The operator command is the way in when nobody can invite: it works only while no admin is
	// active, and it only ever issues an invitation. Nobody is re-activated by an email match.
	n, err := s.st.ActiveAdminCount(ctx, s.org)
	if err != nil {
		return contract.Invitation{}, false, err
	}
	if n > 0 {
		return contract.Invitation{}, false, contract.NewError(contract.CodeConflict,
			"an admin is already active; invite admins from Settings > Organization")
	}
	all, err := s.st.ListInvitations(ctx, s.org)
	if err != nil {
		return contract.Invitation{}, false, err
	}
	now := s.cfg.Now()
	for _, i := range all {
		if i.Email != e || i.StatusAt(now) != contract.InvitationPending {
			continue
		}
		if !access.IsAdmin(i.Role) {
			return contract.Invitation{}, false, contract.NewError(contract.CodeConflict,
				"an open invitation for that email has role "+string(i.Role)+"; revoke it in Settings, then run bootstrap-admin again")
		}
		return i, false, nil
	}
	inv, err = s.invite(ctx, "", e, contract.RoleAdmin)
	return inv, err == nil, err
}

// ResendInvitation replaces the provider invitation of an open (not accepted, not
// revoked) invitation and extends its expiry.
func (s *Service) ResendInvitation(ctx context.Context, p contract.Principal, id string) (contract.Invitation, error) {
	inv, err := s.st.Invitation(ctx, p.OrgID, id)
	if err != nil {
		return contract.Invitation{}, err
	}
	if err := access.MayAssignRole(p.Role, inv.Role); err != nil {
		return contract.Invitation{}, err
	}
	now := s.cfg.Now()
	if st := inv.StatusAt(now); st == contract.InvitationAccepted || st == contract.InvitationRevoked {
		return contract.Invitation{}, contract.NewError(contract.CodeNotFound, "not found")
	}
	if err := s.idp.RevokeInvitation(ctx, inv.ClerkInvitationID); err != nil && contract.AsError(err).Code != contract.CodeNotAvailable {
		return contract.Invitation{}, err
	}
	clerkID, err := s.providerInvite(ctx, inv.Email)
	if err != nil {
		return contract.Invitation{}, err
	}
	if err := s.st.RefreshInvitation(ctx, p.OrgID, id, clerkID, now.Add(identity.InvitationLifetime)); err != nil {
		// The row still names the revoked provider invitation; do not leave the new one emailing the person.
		s.compensate(ctx, "resend: database refresh failed", clerkID)
		return contract.Invitation{}, err
	}
	return s.st.Invitation(ctx, p.OrgID, id)
}

// RevokeInvitation revokes an open invitation. The database row decides access
// (complete-signup reads it), so a provider hiccup cannot leave the invitation usable.
func (s *Service) RevokeInvitation(ctx context.Context, p contract.Principal, id string) error {
	inv, err := s.st.Invitation(ctx, p.OrgID, id)
	if err != nil {
		return err
	}
	if err := access.MayAssignRole(p.Role, inv.Role); err != nil {
		return err
	}
	clerkID, err := s.st.RevokeInvitation(ctx, p.OrgID, id, s.cfg.Now())
	if err != nil {
		return err
	}
	s.compensate(ctx, "revoke: provider revoke failed", clerkID)
	return nil
}

// compensate revokes a provider invitation as best-effort cleanup. The database row is the
// access truth, so a failure changes no access, but it leaves an orphan at the provider, so
// it is logged (with the provider id, never the email) instead of swallowed.
func (s *Service) compensate(ctx context.Context, what, providerID string) {
	if providerID == "" {
		return
	}
	if err := s.idp.RevokeInvitation(ctx, providerID); err != nil && contract.AsError(err).Code != contract.CodeNotAvailable {
		s.cfg.Log.LogAttrs(ctx, slog.LevelWarn, "provider invitation cleanup failed",
			slog.String("op", what), slog.String("provider_invitation_id", providerID), slog.String("code", string(contract.AsError(err).Code)))
	}
}
