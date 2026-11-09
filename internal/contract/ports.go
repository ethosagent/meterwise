package contract

import (
	"context"
	"time"
)

// Role is the org-level role (decision D10).
type Role string

// The two org-level roles.
const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// Valid reports whether r is one of the two roles.
func (r Role) Valid() bool { return r == RoleAdmin || r == RoleMember }

// Roles lists the valid roles in a fixed order.
func Roles() []Role { return []Role{RoleAdmin, RoleMember} }

// InvitationStatuses lists the four statuses in a fixed order.
func InvitationStatuses() []InvitationStatus {
	return []InvitationStatus{InvitationPending, InvitationAccepted, InvitationRevoked, InvitationExpired}
}

// Profile is a user's stored UI state: preferences and the active workspace id
// ("" when unset). The id is as stored and may name a workspace that is gone or
// that the user can no longer read; resolving it is the caller's job.
type Profile struct {
	Preferences       Preferences
	ActiveWorkspaceID string
}

// GrantRow is one granted permission as stored: GroupID is "" for an org default, WorkspaceID is ""
// for "every workspace" (and for org-scoped resources).
type GrantRow struct {
	GroupID      string
	WorkspaceID  string
	ResourceType string
	Action       string
}

// ToGrant is the API form of a stored grant: nil WorkspaceID when the row names no workspace.
func (r GrantRow) ToGrant() Grant {
	g := Grant{ResourceType: r.ResourceType, Action: r.Action}
	if r.WorkspaceID != "" {
		ws := r.WorkspaceID
		g.WorkspaceID = &ws
	}
	return g
}

// GrantSpec is a resource type and action; the taxonomy (package access) decides which exist.
type GrantSpec struct{ ResourceType, Action string }

// Identity is what an IdentityProvider proves about a bearer token. Email and
// EmailVerified come from the session token's claims; an invitation is matched
// only when EmailVerified is true.
type Identity struct {
	Subject       string // provider-scoped stable user id
	Email         string
	EmailVerified bool
	Name          string
	AvatarURL     string
}

// User is the local mirror of an identity (table users).
type User struct {
	ID          string
	ClerkUserID string
	Email       string
	Name        string
	AvatarURL   string
	Deleted     bool
}

// Principal is an authenticated, authorised actor inside the one organization.
type Principal struct {
	UserID string
	OrgID  string
	Email  string
	Role   Role
}

// Session is what the gateway learns from one request: the mirrored user, and
// the principal when the user holds an active membership (Member).
type Session struct {
	Identity  Identity
	User      User
	Member    bool
	Principal Principal
}

// Org is the (single, seeded) organization.
type Org struct {
	ID   string
	Name string
}

// Workspace is the unit of proxying (decision D5). PublicID is the unguessable
// URL id (D6). Retired is true when the id answered by a lookup was retired by a
// URL reset and is still inside its grace window; PublicID is then the retired id.
type Workspace struct {
	ID        string
	OrgID     string
	Name      string
	PublicID  string
	IsDefault bool
	Settings  map[string]any
	CreatedAt time.Time
	Retired   bool
}

// InvitationStatus is the derived state of an invitation.
type InvitationStatus string

// The four invitation states.
const (
	InvitationPending  InvitationStatus = "pending"
	InvitationAccepted InvitationStatus = "accepted"
	InvitationRevoked  InvitationStatus = "revoked"
	InvitationExpired  InvitationStatus = "expired"
)

// Invitation is an admin's invitation of one email into the org.
type Invitation struct {
	ID                string
	Email             string // normalised: trimmed, lower case
	Role              Role
	ClerkInvitationID string
	InvitedBy         string // the inviter's email; empty for the CLI bootstrap
	ExpiresAt         time.Time
	CreatedAt         time.Time
	AcceptedAt        *time.Time
	RevokedAt         *time.Time
}

// StatusAt derives the status at now.
func (i Invitation) StatusAt(now time.Time) InvitationStatus {
	switch {
	case i.AcceptedAt != nil:
		return InvitationAccepted
	case i.RevokedAt != nil:
		return InvitationRevoked
	case !i.ExpiresAt.After(now):
		return InvitationExpired
	}
	return InvitationPending
}

// AuditEvent is one append-only audit record.
type AuditEvent struct {
	At          time.Time
	OrgID       string
	WorkspaceID string // empty for org-level events
	ActorID     string
	Action      string
	Target      string
}

// Price is a per-million-token price in micro-USD for one model.
type Price struct {
	Model            string
	InputPerMTokens  MicroUSD
	OutputPerMTokens MicroUSD
}

// IdentityProvider verifies bearer tokens and manages provider-side invitations
// (decision D3). Verify returns CodeUnauthenticated for any token it rejects.
type IdentityProvider interface {
	Verify(ctx context.Context, bearer string) (Identity, error)
	// CreateInvitation asks the provider to email an invitation and returns its id.
	CreateInvitation(ctx context.Context, email string) (string, error)
	// RevokeInvitation withdraws a provider invitation; an unknown id is not an error.
	RevokeInvitation(ctx context.Context, id string) error
}

// PrincipalResolver turns a verified Identity into a Session: it mirrors the
// user (the lazy upsert) and attaches the principal for an active membership.
// A soft-deleted user is CodeUnauthenticated.
type PrincipalResolver interface {
	Resolve(ctx context.Context, id Identity) (Session, error)
}

// OrgDirectory reads the single organization.
type OrgDirectory interface {
	Org(ctx context.Context) (Org, error)
}

// WorkspaceDirectory resolves workspaces for the data plane. ByPublicID answers
// a retired id with Workspace.Retired set and an unknown one with CodeNotFound.
// Lookups are cached for 30 seconds and busted on update and delete.
type WorkspaceDirectory interface {
	ByPublicID(ctx context.Context, publicID string) (Workspace, error)
	ByID(ctx context.Context, id string) (Workspace, error)
	List(ctx context.Context, p Principal) ([]Workspace, error)
}

// AuditSink appends audit events; implementations never update or delete.
type AuditSink interface {
	Append(ctx context.Context, e AuditEvent) error
}

// Pricer prices token usage from an in-memory snapshot (ruling R11): pure, no I/O.
type Pricer interface {
	Cost(model string, inputTokens, outputTokens int64) (MicroUSD, bool)
}
