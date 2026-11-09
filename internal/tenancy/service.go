package tenancy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/identity"
	"github.com/ethosagent/meterwise/internal/store"
)

const (
	maxNameRunes   = 80
	maxEmailLength = 254
	maxIDAttempts  = 5
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

func invalid(field, msg string) error {
	return contract.NewError(contract.CodeValidationFailed, "request validation failed", field+": "+msg)
}

func notInvited() error {
	return contract.NewError(contract.CodeNotInvited, "this account has not been invited to the organization")
}

// NormalizeEmail trims and lower-cases an email and checks it is a bare address.
// Non-ASCII addresses are refused before any case folding: Unicode folding maps
// some characters to ASCII (U+212A KELVIN SIGN to "k", U+0130 to "i"), which would let a
// different address claim an invitation written for another. Internationalized addresses
// need a proper IDN and Unicode comparison policy before they can be admitted.
func NormalizeEmail(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.IndexFunc(trimmed, func(r rune) bool { return r > unicode.MaxASCII }) >= 0 {
		return "", invalid("email", "must be a plain ASCII email address")
	}
	e := strings.ToLower(trimmed)
	addr, err := mail.ParseAddress(e)
	if e == "" || len(e) > maxEmailLength || err != nil || addr.Address != e || addr.Name != "" {
		return "", invalid("email", "must be a plain email address")
	}
	return e, nil
}

func validName(raw string) (string, error) {
	n := strings.TrimSpace(raw)
	switch {
	case n == "" || utf8.RuneCountInString(n) > maxNameRunes:
		return "", invalid("name", "must be 1-80 characters")
	case strings.IndexFunc(n, unicode.IsControl) >= 0:
		return "", invalid("name", "must not contain control characters")
	}
	return n, nil
}

// Resolve mirrors the identity into users (the lazy upsert: it works with zero
// webhooks) and attaches the principal for an active membership. A soft-deleted
// user is unauthenticated. Membership is read on every call, so removing it
// takes effect on the next request.
func (s *Service) Resolve(ctx context.Context, id contract.Identity) (contract.Session, error) {
	u, err := s.st.UpsertUser(ctx, s.org, id)
	if err != nil {
		return contract.Session{}, err
	}
	if u.Deleted {
		return contract.Session{}, contract.NewError(contract.CodeUnauthenticated, "invalid or expired token")
	}
	sess := contract.Session{Identity: id, User: u}
	role, active, _, err := s.st.Membership(ctx, s.org, u.ID)
	if err != nil {
		return contract.Session{}, err
	}
	if active {
		sess.Member = true
		sess.Principal = contract.Principal{UserID: u.ID, OrgID: s.org, Email: u.Email, Role: role}
	}
	return sess, nil
}

// CompleteSignup turns the pending invitation for the session's verified email
// into an active membership. An existing member is returned unchanged. Anyone
// else, including a user whose email is unverified, is not_invited.
func (s *Service) CompleteSignup(ctx context.Context, sess contract.Session) (contract.Session, error) {
	if sess.Member {
		return sess, nil
	}
	if !sess.Identity.EmailVerified {
		return contract.Session{}, notInvited()
	}
	email, err := NormalizeEmail(sess.Identity.Email)
	if err != nil {
		return contract.Session{}, notInvited()
	}
	role, ok, err := s.st.AcceptOpenInvitation(ctx, s.org, sess.User.ID, email, s.cfg.Now())
	if err != nil {
		return contract.Session{}, err
	}
	if !ok {
		// Two tabs of the same person finish first login together: the loser finds the invitation
		// already used, but the person is a member by then. Re-read before refusing.
		if r, active, _, merr := s.st.Membership(ctx, s.org, sess.User.ID); merr == nil && active {
			sess.Member = true
			sess.Principal = contract.Principal{UserID: sess.User.ID, OrgID: s.org, Email: sess.User.Email, Role: r}
			return sess, nil
		}
		return contract.Session{}, notInvited()
	}
	sess.Member = true
	sess.Principal = contract.Principal{UserID: sess.User.ID, OrgID: s.org, Email: sess.User.Email, Role: role}
	return sess, nil
}

// EnsureMember makes a user an active member with role (the dev-only resolver and tests use it;
// production membership comes from invitations).
func (s *Service) EnsureMember(ctx context.Context, userID string, role contract.Role) error {
	return s.st.UpsertActiveMembership(ctx, s.org, userID, role)
}

// ApplyWebhookUser upserts a user from a Clerk webhook.
func (s *Service) ApplyWebhookUser(ctx context.Context, u identity.WebhookUser) error {
	_, err := s.st.UpsertUser(ctx, s.org, contract.Identity{Subject: u.ClerkUserID, Email: u.Email, EmailVerified: u.EmailVerified, Name: u.Name, AvatarURL: u.AvatarURL})
	return err
}

// DeleteWebhookUser soft-deletes a user and removes their memberships.
func (s *Service) DeleteWebhookUser(ctx context.Context, clerkUserID string) error {
	return s.st.SoftDeleteUser(ctx, s.org, clerkUserID)
}

// Profile returns the caller's stored preferences and the active workspace id.
// An unset or stale (deleted) active workspace falls back to the default one.
func (s *Service) Profile(ctx context.Context, userID string) (contract.Preferences, string, error) {
	prefs, active, err := s.st.Preferences(ctx, s.org, userID)
	if err != nil {
		return contract.Preferences{}, "", err
	}
	list, err := s.st.ListWorkspaces(ctx, s.org)
	if err != nil {
		return contract.Preferences{}, "", err
	}
	fallback := ""
	for _, w := range list {
		if w.ID == active {
			return prefs, active, nil
		}
		if w.IsDefault {
			fallback = w.ID
		}
	}
	return prefs, fallback, nil
}

// Preferences returns the user's UI preferences (the real contract-backed store).
func (s *Service) Preferences(ctx context.Context, userID string) (contract.Preferences, error) {
	p, _, err := s.st.Preferences(ctx, s.org, userID)
	return p, err
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
	p, _, err := s.st.MergePreferences(ctx, s.org, userID, m)
	return p, err
}

// ListWorkspaces returns the org's workspaces.
func (s *Service) ListWorkspaces(ctx context.Context, p contract.Principal) ([]contract.Workspace, error) {
	return s.dir.List(ctx, p)
}

// CreateWorkspace creates a workspace with a fresh CSPRNG public id, honouring MAX_WORKSPACES.
func (s *Service) CreateWorkspace(ctx context.Context, p contract.Principal, name string) (contract.Workspace, error) {
	n, err := validName(name)
	if err != nil {
		return contract.Workspace{}, err
	}
	for range maxIDAttempts {
		pid, err := NewPublicID(s.cfg.Rand)
		if err != nil {
			return contract.Workspace{}, err
		}
		ws, err := s.st.CreateWorkspace(ctx, p.OrgID, n, pid, s.cfg.MaxWorkspaces)
		if errors.Is(err, store.ErrPublicIDTaken) {
			continue
		}
		return ws, err
	}
	return contract.Workspace{}, contract.NewError(contract.CodeInternal, "could not allocate a workspace id")
}

// EnsureDefaults creates the default workspace when the org has none. It is idempotent.
func (s *Service) EnsureDefaults(ctx context.Context) error {
	for range maxIDAttempts {
		pid, err := NewPublicID(s.cfg.Rand)
		if err != nil {
			return err
		}
		_, err = s.st.EnsureDefaultWorkspace(ctx, s.org, DefaultWorkspaceName, pid)
		if errors.Is(err, store.ErrPublicIDTaken) {
			continue
		}
		return err
	}
	return contract.NewError(contract.CodeInternal, "could not allocate a workspace id")
}

// UpdateWorkspace renames a workspace and/or replaces its settings, then busts the directory cache.
func (s *Service) UpdateWorkspace(ctx context.Context, p contract.Principal, id string, req contract.UpdateWorkspaceRequest) (contract.Workspace, error) {
	var name *string
	if req.Name != nil {
		n, err := validName(*req.Name)
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
	if _, _, err := s.st.MergePreferences(ctx, p.OrgID, p.UserID, map[string]any{"activeWorkspaceId": ws.ID}); err != nil {
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
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return contract.Invitation{}, err
	}
	if role != contract.RoleAdmin && role != contract.RoleMember {
		return contract.Invitation{}, invalid("role", "must be admin or member")
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

// CreateInvitation invites an email as the calling admin.
func (s *Service) CreateInvitation(ctx context.Context, p contract.Principal, email string, role contract.Role) (contract.Invitation, error) {
	return s.invite(ctx, p.UserID, email, role)
}

// BootstrapAdmin is the CLI's first-admin invitation. It is idempotent: an open
// invitation for the email is returned with created=false.
func (s *Service) BootstrapAdmin(ctx context.Context, email string) (inv contract.Invitation, created bool, err error) {
	e, err := NormalizeEmail(email)
	if err != nil {
		return contract.Invitation{}, false, err
	}
	all, err := s.st.ListInvitations(ctx, s.org)
	if err != nil {
		return contract.Invitation{}, false, err
	}
	now := s.cfg.Now()
	for _, i := range all {
		if i.Email != e {
			continue
		}
		switch i.StatusAt(now) {
		case contract.InvitationPending:
			if i.Role != contract.RoleAdmin {
				return contract.Invitation{}, false, contract.NewError(contract.CodeConflict,
					"an open invitation for that email has role "+string(i.Role)+"; revoke it in Settings, then run bootstrap-admin again")
			}
			return i, false, nil
		case contract.InvitationAccepted:
			return contract.Invitation{}, false, contract.NewError(contract.CodeConflict,
				"that email already accepted an invitation; invite admins from Settings > Organization")
		}
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
