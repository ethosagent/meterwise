package contract

import "sort"

// Permission names what a caller needs for a route. The empty Permission with
// Public set means no credential is required; any other route must name one.
type Permission string

// The three route classes that are not taxonomy permissions.
const (
	// PermSelf is held by every authenticated user, member or not, and covers the
	// caller's own profile data and the sign-up handshake only.
	PermSelf Permission = "self"
	// PermMember needs an active membership (role member or admin) and nothing more.
	PermMember Permission = "member"
	// PermAdmin needs an active membership with role admin; no grant can confer it.
	PermAdmin Permission = "admin"
)

// Taxonomy permissions, spelled "resource:action". contract cannot import access (H0), so the
// route table names them as strings; a test in internal/access proves each one exists in the
// taxonomy, which stays the only definition of what can be granted.
const (
	PermWorkspaceRead      Permission = "workspace:read"
	PermWorkspaceWrite     Permission = "workspace:write"
	PermWorkspaceManage    Permission = "workspace:manage"
	PermWorkspaceURLRead   Permission = "workspace_url:read"
	PermWorkspaceURLManage Permission = "workspace_url:manage"
	PermSessionRead        Permission = "session:read"
	PermMonitorConfigRead  Permission = "monitor_config:read"
	PermMonitorConfigWrite Permission = "monitor_config:write"
	PermSavingsRead        Permission = "savings:read"
	PermMemberRead         Permission = "member:read"
	PermMemberManage       Permission = "member:manage"
	PermAuditLogRead       Permission = "audit_log:read"
)

// Route is one row of the route-to-permission table. The table is the single
// source for the gateway middleware, openapi.json and the web twin; a handler
// registered without a row is a bug (Plan 01 adds the router test).
type Route struct {
	Method     string     `json:"method"`
	Pattern    string     `json:"pattern"`
	Permission Permission `json:"permission"`
	// WorkspaceParam names the path parameter that carries the id of the workspace the
	// request acts on ("id" in /api/workspaces/{id}); empty when the route is not scoped to
	// one workspace. The gateway reads it, checks the workspace is in the org (404 if not)
	// and evaluates a workspace-scoped permission against it. A later plan whose URL names a
	// resource instead (Plan 05's /api/sessions/{id}) adds a sibling field that names the
	// resource parameter and a resolver from resource id to workspace; rows that use
	// WorkspaceParam do not change.
	WorkspaceParam string `json:"workspaceParam,omitempty"`
	Public         bool   `json:"public"`
	Summary        string `json:"summary"`
	Request        string `json:"request,omitempty"` // DTO name of the JSON body, if any
	Response       string `json:"response"`          // DTO name from DTOs(); empty for a 204
	Status         int    `json:"status,omitempty"`  // success status; 200 when zero
}

var routes = []Route{
	{Method: "GET", Pattern: "/healthz", Public: true, Summary: "Liveness probe", Response: "Health"},
	{Method: "GET", Pattern: "/api/config", Public: true, Summary: "App name and public proxy host for the shell", Response: "AppConfig"},
	{Method: "GET", Pattern: "/api/me/preferences", Permission: PermSelf, Summary: "Read the caller's UI preferences", Response: "Preferences"},
	{Method: "PATCH", Pattern: "/api/me/preferences", Permission: PermSelf, Summary: "Change the caller's UI preferences", Request: "PreferencesPatch", Response: "Preferences"},
	{Method: "GET", Pattern: "/api/me", Permission: PermSelf, Summary: "The caller, role, active workspace and preferences; 403 not_invited without a membership", Response: "Me"},
	{Method: "POST", Pattern: "/api/auth/complete-signup", Permission: PermSelf, Summary: "Turn a pending invitation for the caller's verified email into a membership", Response: "Me"},
	// Plan 03: grants decide (ruling: any member may still create a workspace).
	{Method: "GET", Pattern: "/api/me/permissions", Permission: PermSelf, Summary: "The caller's permission snapshot: admin, or the flattened grants", Response: "PermissionSnapshot"},
	{Method: "GET", Pattern: "/api/workspaces", Permission: PermMember, Summary: "List the workspaces the caller can read (workspace:read)", Response: "WorkspaceList"},
	{Method: "POST", Pattern: "/api/workspaces", Permission: PermMember, Summary: "Create a workspace and its proxy URL", Request: "CreateWorkspaceRequest", Response: "Workspace", Status: 201},
	{Method: "PATCH", Pattern: "/api/workspaces/{id}", Permission: PermWorkspaceWrite, WorkspaceParam: "id", Summary: "Rename a workspace or change its settings", Request: "UpdateWorkspaceRequest", Response: "Workspace"},
	{Method: "DELETE", Pattern: "/api/workspaces/{id}", Permission: PermWorkspaceManage, WorkspaceParam: "id", Summary: "Delete a workspace (never the default)", Status: 204},
	{Method: "POST", Pattern: "/api/workspaces/{id}/switch", Permission: PermWorkspaceRead, WorkspaceParam: "id", Summary: "Make a workspace the caller's active one", Response: "SwitchWorkspaceResponse"},
	{Method: "GET", Pattern: "/api/invitations", Permission: PermMemberManage, Summary: "List invitations", Response: "InvitationList"},
	{Method: "POST", Pattern: "/api/invitations", Permission: PermMemberManage, Summary: "Invite an email into the organization; 409 already_member for an active member; role admin needs an admin", Request: "CreateInvitationRequest", Response: "Invitation", Status: 201},
	{Method: "POST", Pattern: "/api/invitations/{id}/resend", Permission: PermMemberManage, Summary: "Replace a pending invitation with a fresh one", Response: "Invitation"},
	{Method: "DELETE", Pattern: "/api/invitations/{id}", Permission: PermMemberManage, Summary: "Revoke an invitation", Status: 204},
	{Method: "GET", Pattern: "/api/members", Permission: PermMemberRead, Summary: "List the active members", Response: "MemberList"},
	{Method: "PATCH", Pattern: "/api/members/{userId}", Permission: PermAdmin, Summary: "Change a member's role; 409 last_admin when it would leave no admin", Request: "UpdateMemberRequest", Response: "Member"},
	{Method: "DELETE", Pattern: "/api/members/{userId}", Permission: PermAdmin, Summary: "Remove a member; 409 last_admin when it would leave no admin", Status: 204},
	{Method: "GET", Pattern: "/api/groups", Permission: PermMemberRead, Summary: "List groups", Response: "GroupList"},
	{Method: "POST", Pattern: "/api/groups", Permission: PermAdmin, Summary: "Create a group; 409 group_name_taken", Request: "GroupRequest", Response: "Group", Status: 201},
	{Method: "GET", Pattern: "/api/groups/{id}", Permission: PermMemberRead, Summary: "Read a group", Response: "Group"},
	{Method: "PATCH", Pattern: "/api/groups/{id}", Permission: PermAdmin, Summary: "Rename a group; 409 group_name_taken", Request: "GroupRequest", Response: "Group"},
	{Method: "DELETE", Pattern: "/api/groups/{id}", Permission: PermAdmin, Summary: "Delete a group and everything it grants", Status: 204},
	{Method: "GET", Pattern: "/api/groups/{id}/permissions", Permission: PermMemberRead, Summary: "A group's grants", Response: "GrantList"},
	{Method: "PUT", Pattern: "/api/groups/{id}/permissions", Permission: PermAdmin, Summary: "Replace a group's grants in one transaction", Request: "GrantList", Response: "GrantList"},
	{Method: "POST", Pattern: "/api/groups/{id}/workspaces", Permission: PermAdmin, Summary: "Attach a group to a workspace (seeds read grants); idempotent", Request: "AttachWorkspaceRequest", Response: "Group"},
	{Method: "DELETE", Pattern: "/api/groups/{id}/workspaces/{wsId}", Permission: PermAdmin, Summary: "Detach a group from a workspace and drop its grants there", Status: 204},
	{Method: "GET", Pattern: "/api/groups/{id}/members", Permission: PermMemberRead, Summary: "A group's members", Response: "MemberList"},
	{Method: "POST", Pattern: "/api/groups/{id}/members", Permission: PermAdmin, Summary: "Add an active member to a group; idempotent", Request: "AddGroupMemberRequest", Response: "MemberList"},
	{Method: "DELETE", Pattern: "/api/groups/{id}/members/{userId}", Permission: PermAdmin, Summary: "Remove a member from a group", Status: 204},
	{Method: "GET", Pattern: "/api/organization/permissions", Permission: PermMemberRead, Summary: "The org defaults every member gets", Response: "GrantList"},
	{Method: "PUT", Pattern: "/api/organization/permissions", Permission: PermAdmin, Summary: "Replace the org defaults in one transaction", Request: "GrantList", Response: "GrantList"},
	{Method: "POST", Pattern: "/api/webhooks/clerk", Public: true, Summary: "Clerk user webhook; authenticated by its Svix signature, not a bearer token", Status: 204},
	{Method: "GET", Pattern: "/api/v1/version", Public: true, Summary: "Build version", Response: "Version"},
}

// Routes returns a copy of the table sorted by pattern then method.
func Routes() []Route {
	out := append([]Route(nil), routes...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pattern != out[j].Pattern {
			return out[i].Pattern < out[j].Pattern
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// PermissionFor returns the row for a method and pattern.
func PermissionFor(method, pattern string) (Route, bool) {
	for _, r := range routes {
		if r.Method == method && r.Pattern == pattern {
			return r, true
		}
	}
	return Route{}, false
}
