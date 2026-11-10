package tenancy

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/identity"
	"github.com/ethosagent/meterwise/internal/store"
)

// DefaultWorkspaceName names the workspace created at first boot.
const DefaultWorkspaceName = "Default"

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
	return &Service{st: st, idp: idp, dir: NewDirectory(st, cfg.Now, cfg.Rand), cfg: cfg, org: store.OrgID()}
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

// withPublicID runs try with a fresh public id from the service's random source (see WithPublicID).
func (s *Service) withPublicID(try func(publicID string) error) error {
	return WithPublicID(s.cfg.Rand, try)
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
