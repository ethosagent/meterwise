package contract

import "time"

// AuditAction names what an audit row records. The list is fixed: the control plane emits only
// these, GET /api/audit-log?action= accepts only these, and the web twin exports the same list.
type AuditAction string

// The audit actions.
const (
	AuditWorkspaceCreate        AuditAction = "workspace.create"
	AuditWorkspaceUpdate        AuditAction = "workspace.update"
	AuditWorkspaceDelete        AuditAction = "workspace.delete"
	AuditWorkspaceURLReset      AuditAction = "workspace.url_reset"
	AuditWorkspaceURLRetire     AuditAction = "workspace.url_retire_previous"
	AuditWorkspaceAllowedCIDRs  AuditAction = "workspace.allowed_cidrs_update"
	AuditInvitationCreate       AuditAction = "invitation.create"
	AuditInvitationResend       AuditAction = "invitation.resend"
	AuditInvitationRevoke       AuditAction = "invitation.revoke"
	AuditMemberRoleChange       AuditAction = "member.role_change"
	AuditMemberRemove           AuditAction = "member.remove"
	AuditGroupCreate            AuditAction = "group.create"
	AuditGroupUpdate            AuditAction = "group.update"
	AuditGroupDelete            AuditAction = "group.delete"
	AuditGroupPermissionsUpdate AuditAction = "group.permissions_update"
	AuditGroupWorkspaceAttach   AuditAction = "group.workspace_attach"
	AuditGroupWorkspaceDetach   AuditAction = "group.workspace_detach"
	AuditGroupMemberAdd         AuditAction = "group.member_add"
	AuditGroupMemberRemove      AuditAction = "group.member_remove"
	AuditGroupMembersUpdate     AuditAction = "group.members_update"
	AuditOrgPermissionsUpdate   AuditAction = "org.permissions_update"
	AuditWorkspaceMonitors      AuditAction = "workspace.monitors_update"
	AuditMonitorEventFeedback   AuditAction = "monitor_event.feedback"
)

// AuditActions lists every action in a fixed order.
func AuditActions() []AuditAction {
	return []AuditAction{
		AuditWorkspaceCreate, AuditWorkspaceUpdate, AuditWorkspaceDelete, AuditWorkspaceURLReset, AuditWorkspaceURLRetire,
		AuditWorkspaceAllowedCIDRs, AuditInvitationCreate, AuditInvitationResend, AuditInvitationRevoke, AuditMemberRoleChange,
		AuditMemberRemove, AuditGroupCreate, AuditGroupUpdate, AuditGroupDelete, AuditGroupPermissionsUpdate,
		AuditGroupWorkspaceAttach, AuditGroupWorkspaceDetach, AuditGroupMemberAdd, AuditGroupMemberRemove,
		AuditGroupMembersUpdate, AuditOrgPermissionsUpdate, AuditWorkspaceMonitors, AuditMonitorEventFeedback,
	}
}

// Valid reports whether a is one of the audit actions.
func (a AuditAction) Valid() bool {
	for _, v := range AuditActions() {
		if a == v {
			return true
		}
	}
	return false
}

// AuditEvent is one append-only audit record. WorkspaceID, ActorID, ResourceID and IP are empty
// when they do not apply. Metadata holds small non-sensitive facts (a role, a count, a field
// name); it never carries a public workspace id or a proxy URL.
type AuditEvent struct {
	At           time.Time
	OrgID        string
	WorkspaceID  string
	ActorID      string
	Action       AuditAction
	ResourceType string
	ResourceID   string
	IP           string
	Metadata     map[string]any
}

// AuditQuery filters and pages the audit log. Cursor is opaque ("" starts at the newest row).
type AuditQuery struct {
	Action      AuditAction
	WorkspaceID string
	Cursor      string
	Limit       int
}

// AuditRecord is a stored audit row with its actor resolved.
type AuditRecord struct {
	ID           int64
	At           time.Time
	Action       AuditAction
	ResourceType string
	ResourceID   string
	WorkspaceID  string
	ActorID      string
	ActorEmail   string
	ActorName    string
	IP           string
	Metadata     map[string]any
}

// AuditPage is one page of records, newest first; NextCursor is "" on the last page.
type AuditPage struct {
	Items      []AuditRecord
	NextCursor string
}

// AuditInput is what a route's audit extractors see after the handler succeeded: the path
// values, the caller, and the decoded request and response bodies (the DTO the row names; nil
// when the row has none).
type AuditInput struct {
	Path      func(name string) string
	Principal Principal
	Req, Resp any
}

// AuditSpec declares, on the route row, the audit event a successful call emits. The one handler
// wrapper in internal/api reads it; a handler never writes an audit row itself. WorkspaceID and
// Meta are optional. Metadata is small and never carries a workspace public id or URL.
type AuditSpec struct {
	Action       AuditAction
	ResourceType string
	ResourceID   func(AuditInput) string
	WorkspaceID  func(AuditInput) string
	Meta         func(AuditInput) map[string]any
}
