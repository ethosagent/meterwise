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
	// Audit is the event emitted after a successful call; NoAudit says a mutating route is
	// deliberately not audited. Every non-GET row has one of the two, a GET row neither.
	Audit   *AuditSpec `json:"-"`
	NoAudit bool       `json:"-"`
}

// pathID extracts a path parameter as the audited resource or workspace id.
func pathID(name string) func(AuditInput) string {
	return func(in AuditInput) string { return in.Path(name) }
}

func wsViewID(in AuditInput) string { return in.Resp.(WorkspaceView).ID }

func roleMeta(in AuditInput) map[string]any {
	switch r := in.Req.(type) {
	case CreateInvitationRequest:
		return map[string]any{"role": string(r.Role)}
	case UpdateMemberRequest:
		return map[string]any{"role": string(r.Role)}
	}
	return nil
}

// updatedFields lists which parts of a workspace PATCH were present.
func updatedFields(in AuditInput) map[string]any {
	req := in.Req.(UpdateWorkspaceRequest)
	fields := []string{}
	if req.Name != nil {
		fields = append(fields, "name")
	}
	if req.Settings != nil {
		fields = append(fields, "settings")
	}
	return map[string]any{"fields": fields}
}

// graceMeta records the grace window a reset used, the default when the body left it out.
func graceMeta(in AuditInput) map[string]any {
	grace := DefaultGraceMinutes
	if g := in.Req.(ResetURLRequest).GraceMinutes; g != nil {
		grace = *g
	}
	return map[string]any{"graceMinutes": grace}
}

// auditSpec is the common shape: a resource type, its id from the path, no workspace, no metadata.
func auditSpec(a AuditAction, resourceType, param string) *AuditSpec {
	sp := &AuditSpec{Action: a, ResourceType: resourceType}
	if param != "" {
		sp.ResourceID = pathID(param)
	}
	return sp
}

// wsAuditSpec is auditSpec for a workspace row: the workspace id is the resource id too.
func wsAuditSpec(a AuditAction, meta func(AuditInput) map[string]any) *AuditSpec {
	return &AuditSpec{Action: a, ResourceType: "workspace", ResourceID: pathID("id"), WorkspaceID: pathID("id"), Meta: meta}
}

// withID replaces the resource id extractor (a create row reads the id from the response).
func (s *AuditSpec) withID(f func(AuditInput) string) *AuditSpec {
	s.ResourceID = f
	return s
}

// withWorkspace sets the workspace id extractor.
func (s *AuditSpec) withWorkspace(f func(AuditInput) string) *AuditSpec {
	s.WorkspaceID = f
	return s
}

// withMeta returns s with its metadata extractor set.
func (s *AuditSpec) withMeta(m func(AuditInput) map[string]any) *AuditSpec {
	s.Meta = m
	return s
}

func countOf(n func(AuditInput) int) func(AuditInput) map[string]any {
	return func(in AuditInput) map[string]any { return map[string]any{"count": n(in)} }
}

var routes = []Route{
	{Method: "GET", Pattern: "/healthz", Public: true, Summary: "Liveness probe", Response: "Health"},
	{Method: "GET", Pattern: "/api/config", Public: true, Summary: "App name and public proxy host for the shell", Response: "AppConfig"},
	{Method: "GET", Pattern: "/api/me/preferences", Permission: PermSelf, Summary: "Read the caller's UI preferences", Response: "Preferences"},
	{Method: "PATCH", Pattern: "/api/me/preferences", Permission: PermSelf, Summary: "Change the caller's UI preferences", Request: "PreferencesPatch", Response: "Preferences", NoAudit: true},
	{Method: "GET", Pattern: "/api/me", Permission: PermSelf, Summary: "The caller, role, active workspace and preferences; 403 not_invited without a membership", Response: "Me"},
	{Method: "POST", Pattern: "/api/auth/complete-signup", Permission: PermSelf, Summary: "Turn a pending invitation for the caller's verified email into a membership", Response: "Me", NoAudit: true},
	// Plan 03: grants decide (ruling: any member may still create a workspace).
	{Method: "GET", Pattern: "/api/me/permissions", Permission: PermSelf, Summary: "The caller's permission snapshot: admin, or the flattened grants", Response: "PermissionSnapshot"},
	{Method: "GET", Pattern: "/api/workspaces", Permission: PermMember, Summary: "List the workspaces the caller can read (workspace:read)", Response: "WorkspaceList"},
	{Method: "POST", Pattern: "/api/workspaces", Permission: PermMember, Summary: "Create a workspace and its proxy URL", Request: "CreateWorkspaceRequest", Response: "Workspace", Status: 201, Audit: &AuditSpec{Action: AuditWorkspaceCreate, ResourceType: "workspace", ResourceID: wsViewID, WorkspaceID: wsViewID}},
	{Method: "PATCH", Pattern: "/api/workspaces/{id}", Permission: PermWorkspaceWrite, WorkspaceParam: "id", Summary: "Rename a workspace or change its settings", Request: "UpdateWorkspaceRequest", Response: "Workspace", Audit: wsAuditSpec(AuditWorkspaceUpdate, updatedFields)},
	{Method: "DELETE", Pattern: "/api/workspaces/{id}", Permission: PermWorkspaceManage, WorkspaceParam: "id", Summary: "Delete a workspace (never the default)", Status: 204, Audit: wsAuditSpec(AuditWorkspaceDelete, nil)},
	{Method: "POST", Pattern: "/api/workspaces/{id}/switch", Permission: PermWorkspaceRead, WorkspaceParam: "id", Summary: "Make a workspace the caller's active one", Response: "SwitchWorkspaceResponse", NoAudit: true},
	{Method: "GET", Pattern: "/api/workspaces/{id}/setup", Permission: PermWorkspaceURLRead, WorkspaceParam: "id", Summary: "The proxy URL, ready-made client snippets and the connection status of a workspace", Response: "Setup"},
	{Method: "POST", Pattern: "/api/workspaces/{id}/url/reset", Permission: PermWorkspaceURLManage, WorkspaceParam: "id", Summary: "Give the workspace a new proxy URL; the old one keeps working for graceMinutes (0-1440, default 10)", Request: "ResetURLRequest", Response: "ResetURLResponse", Audit: wsAuditSpec(AuditWorkspaceURLReset, graceMeta)},
	{Method: "POST", Pattern: "/api/workspaces/{id}/url/retire-previous", Permission: PermWorkspaceURLManage, WorkspaceParam: "id", Summary: "End the grace window of every previous proxy URL now", Status: 204, Audit: wsAuditSpec(AuditWorkspaceURLRetire, nil)},
	{Method: "PUT", Pattern: "/api/workspaces/{id}/url/allowed-cidrs", Permission: PermWorkspaceURLManage, WorkspaceParam: "id", Summary: "Set the source networks the proxy accepts for the workspace ([] means any); 422 for a bad CIDR", Request: "AllowedCIDRs", Response: "AllowedCIDRs", Audit: wsAuditSpec(AuditWorkspaceAllowedCIDRs, countOf(func(in AuditInput) int { return len(in.Resp.(AllowedCIDRs).CIDRs) }))},
	{Method: "GET", Pattern: "/api/audit-log", Permission: PermAuditLogRead, Summary: "The audit log, newest first; filter by action and workspaceId, page with the opaque cursor; 422 for an unknown action", Response: "AuditList"},
	{Method: "GET", Pattern: "/api/invitations", Permission: PermMemberManage, Summary: "List invitations", Response: "InvitationList"},
	{Method: "POST", Pattern: "/api/invitations", Permission: PermMemberManage, Summary: "Invite an email into the organization; 409 already_member for an active member; role admin needs an admin", Request: "CreateInvitationRequest", Response: "Invitation", Status: 201, Audit: auditSpec(AuditInvitationCreate, "invitation", "").withID(func(in AuditInput) string { return in.Resp.(InvitationView).ID }).withMeta(roleMeta)},
	{Method: "POST", Pattern: "/api/invitations/{id}/resend", Permission: PermMemberManage, Summary: "Replace a pending invitation with a fresh one", Response: "Invitation", Audit: auditSpec(AuditInvitationResend, "invitation", "id")},
	{Method: "DELETE", Pattern: "/api/invitations/{id}", Permission: PermMemberManage, Summary: "Revoke an invitation", Status: 204, Audit: auditSpec(AuditInvitationRevoke, "invitation", "id")},
	{Method: "GET", Pattern: "/api/members", Permission: PermMemberRead, Summary: "List the active members", Response: "MemberList"},
	{Method: "PATCH", Pattern: "/api/members/{userId}", Permission: PermAdmin, Summary: "Change a member's role; 409 last_admin when it would leave no admin", Request: "UpdateMemberRequest", Response: "Member", Audit: auditSpec(AuditMemberRoleChange, "member", "userId").withMeta(roleMeta)},
	{Method: "DELETE", Pattern: "/api/members/{userId}", Permission: PermAdmin, Summary: "Remove a member; 409 last_admin when it would leave no admin", Status: 204, Audit: auditSpec(AuditMemberRemove, "member", "userId")},
	{Method: "GET", Pattern: "/api/groups", Permission: PermMemberRead, Summary: "List groups", Response: "GroupList"},
	{Method: "POST", Pattern: "/api/groups", Permission: PermAdmin, Summary: "Create a group; 409 group_name_taken", Request: "GroupRequest", Response: "Group", Status: 201, Audit: auditSpec(AuditGroupCreate, "group", "").withID(func(in AuditInput) string { return in.Resp.(Group).ID })},
	{Method: "GET", Pattern: "/api/groups/{id}", Permission: PermMemberRead, Summary: "Read a group", Response: "Group"},
	{Method: "PATCH", Pattern: "/api/groups/{id}", Permission: PermAdmin, Summary: "Rename a group; 409 group_name_taken", Request: "GroupRequest", Response: "Group", Audit: auditSpec(AuditGroupUpdate, "group", "id").withMeta(func(AuditInput) map[string]any { return map[string]any{"fields": []string{"name"}} })},
	{Method: "DELETE", Pattern: "/api/groups/{id}", Permission: PermAdmin, Summary: "Delete a group and everything it grants", Status: 204, Audit: auditSpec(AuditGroupDelete, "group", "id")},
	{Method: "GET", Pattern: "/api/groups/{id}/permissions", Permission: PermMemberRead, Summary: "A group's grants", Response: "GrantList"},
	{Method: "PUT", Pattern: "/api/groups/{id}/permissions", Permission: PermAdmin, Summary: "Replace a group's grants in one transaction", Request: "GrantList", Response: "GrantList", Audit: auditSpec(AuditGroupPermissionsUpdate, "group", "id").withMeta(countOf(func(in AuditInput) int { return len(in.Resp.(GrantList).Grants) }))},
	{Method: "POST", Pattern: "/api/groups/{id}/workspaces", Permission: PermAdmin, Summary: "Attach a group to a workspace (seeds read grants); idempotent", Request: "AttachWorkspaceRequest", Response: "Group", Audit: auditSpec(AuditGroupWorkspaceAttach, "group", "id").withWorkspace(func(in AuditInput) string { return in.Req.(AttachWorkspaceRequest).WorkspaceID })},
	{Method: "DELETE", Pattern: "/api/groups/{id}/workspaces/{wsId}", Permission: PermAdmin, Summary: "Detach a group from a workspace and drop its grants there", Status: 204, Audit: auditSpec(AuditGroupWorkspaceDetach, "group", "id").withWorkspace(pathID("wsId"))},
	{Method: "GET", Pattern: "/api/groups/{id}/members", Permission: PermMemberRead, Summary: "A group's members", Response: "MemberList"},
	{Method: "POST", Pattern: "/api/groups/{id}/members", Permission: PermAdmin, Summary: "Add an active member to a group; idempotent", Request: "AddGroupMemberRequest", Response: "MemberList", Audit: auditSpec(AuditGroupMemberAdd, "group", "id").withMeta(func(in AuditInput) map[string]any {
		return map[string]any{"userId": in.Req.(AddGroupMemberRequest).UserID}
	})},
	{Method: "PUT", Pattern: "/api/groups/{id}/members", Permission: PermAdmin, Summary: "Replace a group's member set in one transaction; 422 unless every id is an active member", Request: "ReplaceGroupMembersRequest", Response: "MemberList", Audit: auditSpec(AuditGroupMembersUpdate, "group", "id").withMeta(countOf(func(in AuditInput) int { return len(in.Resp.(MemberList).Items) }))},
	{Method: "DELETE", Pattern: "/api/groups/{id}/members/{userId}", Permission: PermAdmin, Summary: "Remove a member from a group", Status: 204, Audit: auditSpec(AuditGroupMemberRemove, "group", "id").withMeta(func(in AuditInput) map[string]any { return map[string]any{"userId": in.Path("userId")} })},
	{Method: "GET", Pattern: "/api/organization/permissions", Permission: PermMemberRead, Summary: "The org defaults every member gets", Response: "GrantList"},
	{Method: "PUT", Pattern: "/api/organization/permissions", Permission: PermAdmin, Summary: "Replace the org defaults in one transaction", Request: "GrantList", Response: "GrantList", Audit: &AuditSpec{Action: AuditOrgPermissionsUpdate, ResourceType: "organization", ResourceID: func(in AuditInput) string { return in.Principal.OrgID }, Meta: countOf(func(in AuditInput) int { return len(in.Resp.(GrantList).Grants) })}},
	{Method: "POST", Pattern: "/api/webhooks/clerk", Public: true, Summary: "Clerk user webhook; authenticated by its Svix signature, not a bearer token", Status: 204, NoAudit: true},
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
