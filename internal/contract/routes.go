package contract

import "sort"

// Permission names what a caller needs for a route. The empty Permission with
// Public set means no credential is required; any other route must name one.
type Permission string

// The permissions of Plan 02; Plan 03 maps them into the RBAC taxonomy.
const (
	// PermSelf is held by every authenticated user, member or not, and covers the
	// caller's own profile data and the sign-up handshake only.
	PermSelf Permission = "self"
	// PermMember needs an active membership (role member or admin).
	PermMember Permission = "member"
	// PermAdmin needs an active membership with role admin.
	PermAdmin Permission = "admin"
)

// Allows is the one role check of the gateway: it reports whether a principal of
// role may use a route that needs p. PermSelf is decided before a role exists.
func Allows(role Role, p Permission) bool {
	switch p {
	case PermSelf, PermMember:
		return role == RoleMember || role == RoleAdmin
	case PermAdmin:
		return role == RoleAdmin
	}
	return false
}

// Route is one row of the route-to-permission table. The table is the single
// source for the gateway middleware, openapi.json and the web twin; a handler
// registered without a row is a bug (Plan 01 adds the router test).
type Route struct {
	Method     string     `json:"method"`
	Pattern    string     `json:"pattern"`
	Permission Permission `json:"permission"`
	Public     bool       `json:"public"`
	Summary    string     `json:"summary"`
	Request    string     `json:"request,omitempty"` // DTO name of the JSON body, if any
	Response   string     `json:"response"`          // DTO name from DTOs(); empty for a 204
	Status     int        `json:"status,omitempty"`  // success status; 200 when zero
}

var routes = []Route{
	{Method: "GET", Pattern: "/healthz", Public: true, Summary: "Liveness probe", Response: "Health"},
	{Method: "GET", Pattern: "/api/config", Public: true, Summary: "App name and public proxy host for the shell", Response: "AppConfig"},
	{Method: "GET", Pattern: "/api/me/preferences", Permission: PermSelf, Summary: "Read the caller's UI preferences", Response: "Preferences"},
	{Method: "PATCH", Pattern: "/api/me/preferences", Permission: PermSelf, Summary: "Change the caller's UI preferences", Request: "PreferencesPatch", Response: "Preferences"},
	{Method: "GET", Pattern: "/api/me", Permission: PermSelf, Summary: "The caller, role, active workspace and preferences; 403 not_invited without a membership", Response: "Me"},
	{Method: "POST", Pattern: "/api/auth/complete-signup", Permission: PermSelf, Summary: "Turn a pending invitation for the caller's verified email into a membership", Response: "Me"},
	// Ruling (Plan 02 integration): any member may create a workspace; rename and delete are
	// admin-only. Plan 03 replaces these rows with grants.
	{Method: "GET", Pattern: "/api/workspaces", Permission: PermMember, Summary: "List the organization's workspaces", Response: "WorkspaceList"},
	{Method: "POST", Pattern: "/api/workspaces", Permission: PermMember, Summary: "Create a workspace and its proxy URL", Request: "CreateWorkspaceRequest", Response: "Workspace", Status: 201},
	{Method: "PATCH", Pattern: "/api/workspaces/{id}", Permission: PermAdmin, Summary: "Rename a workspace or change its settings", Request: "UpdateWorkspaceRequest", Response: "Workspace"},
	{Method: "DELETE", Pattern: "/api/workspaces/{id}", Permission: PermAdmin, Summary: "Delete a workspace (never the default)", Status: 204},
	{Method: "POST", Pattern: "/api/workspaces/{id}/switch", Permission: PermMember, Summary: "Make a workspace the caller's active one", Response: "SwitchWorkspaceResponse"},
	{Method: "GET", Pattern: "/api/invitations", Permission: PermAdmin, Summary: "List invitations", Response: "InvitationList"},
	{Method: "POST", Pattern: "/api/invitations", Permission: PermAdmin, Summary: "Invite an email into the organization", Request: "CreateInvitationRequest", Response: "Invitation", Status: 201},
	{Method: "POST", Pattern: "/api/invitations/{id}/resend", Permission: PermAdmin, Summary: "Replace a pending invitation with a fresh one", Response: "Invitation"},
	{Method: "DELETE", Pattern: "/api/invitations/{id}", Permission: PermAdmin, Summary: "Revoke an invitation", Status: 204},
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
