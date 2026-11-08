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

// Identity is what an IdentityProvider proves about a bearer token.
type Identity struct {
	Subject string // provider-scoped stable user id
	Email   string
}

// Principal is an authenticated, authorised actor inside the one organization.
type Principal struct {
	UserID string
	OrgID  string
	Email  string
	Role   Role
}

// Org is the (single, seeded) organization.
type Org struct {
	ID   string
	Name string
}

// Workspace is the unit of proxying (decision D5). PublicID is the unguessable
// URL id (D6); it is resolved only through WorkspaceDirectory.
type Workspace struct {
	ID       string
	OrgID    string
	Name     string
	PublicID string
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

// IdentityProvider verifies a bearer token (decision D3; Clerk adapter later).
type IdentityProvider interface {
	Verify(ctx context.Context, bearer string) (Identity, error)
}

// PrincipalResolver turns a verified Identity into a Principal of the org.
type PrincipalResolver interface {
	Resolve(ctx context.Context, id Identity) (Principal, error)
}

// OrgDirectory reads the single organization.
type OrgDirectory interface {
	Org(ctx context.Context) (Org, error)
}

// WorkspaceDirectory resolves workspaces; PublicID lookups return CodeGone for
// a retired id inside its grace window and CodeNotFound otherwise.
type WorkspaceDirectory interface {
	ByPublicID(ctx context.Context, publicID string) (Workspace, error)
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
