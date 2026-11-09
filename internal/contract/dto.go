package contract

import "reflect"

// Health is the /healthz body.
type Health struct {
	Status string `json:"status"`
}

// Version is the /api/v1/version body.
type Version struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// ErrorBody is the JSON body of every API error: {code, message, details}.
// Details is always an object (empty when there is nothing to add).
type ErrorBody struct {
	Code    Code           `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// AppConfig is the GET /api/config body: what the shell needs before sign-in.
type AppConfig struct {
	AppName         string `json:"appName"`
	ProxyPublicHost string `json:"proxyPublicHost"`
}

// Theme is the colour scheme a user prefers.
type Theme string

// The three themes; "system" follows prefers-color-scheme.
const (
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
	ThemeSystem Theme = "system"
)

// Themes lists every valid Theme in a fixed order.
func Themes() []Theme { return []Theme{ThemeLight, ThemeDark, ThemeSystem} }

// Valid reports whether t is one of the three themes.
func (t Theme) Valid() bool {
	for _, v := range Themes() {
		if t == v {
			return true
		}
	}
	return false
}

// Preferences is the GET and PATCH /api/me/preferences response.
type Preferences struct {
	Theme         Theme `json:"theme"`
	ReducedMotion bool  `json:"reducedMotion"`
}

// PreferencesPatch is the PATCH /api/me/preferences request: only the fields sent change.
type PreferencesPatch struct {
	Theme         *Theme `json:"theme,omitempty"`
	ReducedMotion *bool  `json:"reducedMotion,omitempty"`
}

// MeUser is the user part of Me.
type MeUser struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
}

// Me is the GET /api/me and POST /api/auth/complete-signup response.
type Me struct {
	User              MeUser      `json:"user"`
	OrgID             string      `json:"orgId"`
	Role              Role        `json:"role"`
	ActiveWorkspaceID *string     `json:"activeWorkspaceId"` // null when the caller can read no workspace
	Preferences       Preferences `json:"preferences"`
}

// WorkspaceView is the Workspace API type. PublicID, ProxyURL (https://{proxyPublicHost}/w/{publicId})
// and AllowedClientCidrs are the workspace's credential and its guard: all three are null when the
// caller lacks workspace_url:read on the workspace, in every response that carries a Workspace.
type WorkspaceView struct {
	ID                 string         `json:"id"`
	PublicID           *string        `json:"publicId"`
	Name               string         `json:"name"`
	IsDefault          bool           `json:"isDefault"`
	ProxyURL           *string        `json:"proxyUrl"`
	AllowedClientCidrs *[]string      `json:"allowedClientCidrs"`
	Settings           map[string]any `json:"settings"`
	CreatedAt          string         `json:"createdAt"`
}

// WorkspaceList is the GET /api/workspaces response.
type WorkspaceList struct {
	Items []WorkspaceView `json:"items"`
}

// CreateWorkspaceRequest is the POST /api/workspaces body.
type CreateWorkspaceRequest struct {
	Name string `json:"name"`
}

// UpdateWorkspaceRequest is the PATCH /api/workspaces/{id} body: only fields sent change.
type UpdateWorkspaceRequest struct {
	Name     *string        `json:"name,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

// SwitchWorkspaceResponse is the POST /api/workspaces/{id}/switch response.
type SwitchWorkspaceResponse struct {
	ActiveWorkspaceID string `json:"activeWorkspaceId"`
}

// InvitationView is the Invitation API type.
type InvitationView struct {
	ID        string           `json:"id"`
	Email     string           `json:"email"`
	Role      Role             `json:"role"`
	Status    InvitationStatus `json:"status"`
	InvitedBy string           `json:"invitedBy"`
	ExpiresAt string           `json:"expiresAt"`
	CreatedAt string           `json:"createdAt"`
}

// InvitationList is the GET /api/invitations response.
type InvitationList struct {
	Items []InvitationView `json:"items"`
}

// CreateInvitationRequest is the POST /api/invitations body.
type CreateInvitationRequest struct {
	Email string `json:"email"`
	Role  Role   `json:"role"`
}

// Grant is one permission: a resource type and action, on one workspace or (null) on every
// workspace / the org. Whether the resource type is workspace- or org-scoped is fixed by the
// taxonomy, not by the grant.
type Grant struct {
	WorkspaceID  *string `json:"workspaceId"`
	ResourceType string  `json:"resourceType"`
	Action       string  `json:"action"`
}

// WorkspaceOrEmpty is the grant's workspace id, "" for one on every workspace (or an org-scoped one).
func (g Grant) WorkspaceOrEmpty() string {
	if g.WorkspaceID == nil {
		return ""
	}
	return *g.WorkspaceID
}

// Key identifies the grant: resource type, action and workspace id, so equal grants share a key.
func (g Grant) Key() string {
	return g.ResourceType + "\x00" + g.Action + "\x00" + g.WorkspaceOrEmpty()
}

// GrantList is the grants body of the group and org-default endpoints, request and response.
type GrantList struct {
	Grants []Grant `json:"grants"`
}

// PermissionSnapshot is GET /api/me/permissions: Admin true (and no grants) for an admin,
// otherwise the flattened union of the caller's group grants and the org defaults.
type PermissionSnapshot struct {
	Admin  bool    `json:"admin"`
	Grants []Grant `json:"grants"`
}

// Member is an active membership as the API shows it.
type Member struct {
	UserID    string   `json:"userId"`
	Email     string   `json:"email"`
	Name      string   `json:"name"`
	AvatarURL string   `json:"avatarUrl"`
	Role      Role     `json:"role"`
	GroupIDs  []string `json:"groupIds"`
	JoinedAt  string   `json:"joinedAt"`
}

// MemberList is a list of members.
type MemberList struct {
	Items []Member `json:"items"`
}

// UpdateMemberRequest is the PATCH /api/members/{userId} body.
type UpdateMemberRequest struct {
	Role Role `json:"role"`
}

// Group is a set of members with grants on the workspaces it is attached to.
type Group struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	MemberCount  int      `json:"memberCount"`
	WorkspaceIDs []string `json:"workspaceIds"`
	CreatedAt    string   `json:"createdAt"`
}

// GroupList is the GET /api/groups response.
type GroupList struct {
	Items []Group `json:"items"`
}

// GroupRequest is the POST and PATCH /api/groups body.
type GroupRequest struct {
	Name string `json:"name"`
}

// AttachWorkspaceRequest is the POST /api/groups/{id}/workspaces body.
type AttachWorkspaceRequest struct {
	WorkspaceID string `json:"workspaceId"`
}

// AddGroupMemberRequest is the POST /api/groups/{id}/members body.
type AddGroupMemberRequest struct {
	UserID string `json:"userId"`
}

// ReplaceGroupMembersRequest is the PUT /api/groups/{id}/members body: the group's whole member set.
type ReplaceGroupMembersRequest struct {
	UserIDs []string `json:"userIds"`
}

// SetupSnippets are the ready-to-paste client configurations, rendered on the server with the
// workspace URL filled in. OpencodeJSON is valid JSON (an opencode provider block).
type SetupSnippets struct {
	OpenaiEnv     string `json:"openaiEnv"`
	AnthropicEnv  string `json:"anthropicEnv"`
	OpencodeJSON  string `json:"opencodeJson"`
	ClaudeCodeEnv string `json:"claudeCodeEnv"`
	Curl          string `json:"curl"`
}

// SetupStatus tells whether traffic has arrived. FirstCallAt is null and CallsToday 0 until the
// ledger (Plan 05) records calls; LastCallAt reads workspaces.last_call_at.
type SetupStatus struct {
	FirstCallAt *string `json:"firstCallAt"`
	LastCallAt  *string `json:"lastCallAt"`
	CallsToday  int     `json:"callsToday"`
}

// Setup is the GET /api/workspaces/{id}/setup response.
type Setup struct {
	ProxyURL string        `json:"proxyUrl"`
	Snippets SetupSnippets `json:"snippets"`
	Status   SetupStatus   `json:"status"`
}

// ResetURLRequest is the POST /api/workspaces/{id}/url/reset body: how long the old URL keeps
// working, in minutes (0 to 1440; default 10).
type ResetURLRequest struct {
	GraceMinutes *int `json:"graceMinutes,omitempty"`
}

// ResetURLResponse is the reset result: the new URL and when the previous one stops answering.
// ProxyURL is null unless the caller also holds workspace_url:read on the workspace: managing the
// URL does not entitle anyone to see it.
type ResetURLResponse struct {
	ProxyURL           *string `json:"proxyUrl"`
	PreviousGraceUntil string  `json:"previousGraceUntil"`
}

// AllowedCIDRs is the PUT /api/workspaces/{id}/url/allowed-cidrs body and response: the source
// networks the data plane accepts for the workspace ([] means any).
type AllowedCIDRs struct {
	CIDRs []string `json:"cidrs"`
}

// AuditActor is who did it, when the actor is a known user.
type AuditActor struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
	Name   string `json:"name"`
}

// AuditEntry is one audit-log row as the API shows it.
type AuditEntry struct {
	ID           string         `json:"id"`
	At           string         `json:"at"`
	Actor        *AuditActor    `json:"actor"`
	Action       AuditAction    `json:"action"`
	ResourceType string         `json:"resourceType"`
	ResourceID   *string        `json:"resourceId"`
	WorkspaceID  *string        `json:"workspaceId"`
	IP           *string        `json:"ip"`
	Metadata     map[string]any `json:"metadata"`
}

// AuditList is the GET /api/audit-log response: newest first, NextCursor null on the last page.
type AuditList struct {
	Items      []AuditEntry `json:"items"`
	NextCursor *string      `json:"nextCursor"`
}

// DTO is a named API type; the generators walk its fields by reflection.
type DTO struct {
	Name string
	Type reflect.Type
}

// DTOs lists every API type exported to the web twin and openapi.json, in a
// fixed order. A new DTO is added here, nowhere else.
func DTOs() []DTO {
	return []DTO{
		{"Health", reflect.TypeFor[Health]()},
		{"Version", reflect.TypeFor[Version]()},
		{"ErrorBody", reflect.TypeFor[ErrorBody]()},
		{"AppConfig", reflect.TypeFor[AppConfig]()},
		{"Preferences", reflect.TypeFor[Preferences]()},
		{"PreferencesPatch", reflect.TypeFor[PreferencesPatch]()},
		{"Me", reflect.TypeFor[Me]()},
		{"MeUser", reflect.TypeFor[MeUser]()},
		{"Workspace", reflect.TypeFor[WorkspaceView]()},
		{"WorkspaceList", reflect.TypeFor[WorkspaceList]()},
		{"CreateWorkspaceRequest", reflect.TypeFor[CreateWorkspaceRequest]()},
		{"UpdateWorkspaceRequest", reflect.TypeFor[UpdateWorkspaceRequest]()},
		{"SwitchWorkspaceResponse", reflect.TypeFor[SwitchWorkspaceResponse]()},
		{"Invitation", reflect.TypeFor[InvitationView]()},
		{"InvitationList", reflect.TypeFor[InvitationList]()},
		{"CreateInvitationRequest", reflect.TypeFor[CreateInvitationRequest]()},
		{"Grant", reflect.TypeFor[Grant]()},
		{"GrantList", reflect.TypeFor[GrantList]()},
		{"PermissionSnapshot", reflect.TypeFor[PermissionSnapshot]()},
		{"Member", reflect.TypeFor[Member]()},
		{"MemberList", reflect.TypeFor[MemberList]()},
		{"UpdateMemberRequest", reflect.TypeFor[UpdateMemberRequest]()},
		{"Group", reflect.TypeFor[Group]()},
		{"GroupList", reflect.TypeFor[GroupList]()},
		{"GroupRequest", reflect.TypeFor[GroupRequest]()},
		{"AttachWorkspaceRequest", reflect.TypeFor[AttachWorkspaceRequest]()},
		{"AddGroupMemberRequest", reflect.TypeFor[AddGroupMemberRequest]()},
		{"ReplaceGroupMembersRequest", reflect.TypeFor[ReplaceGroupMembersRequest]()},
		{"SetupSnippets", reflect.TypeFor[SetupSnippets]()},
		{"SetupStatus", reflect.TypeFor[SetupStatus]()},
		{"Setup", reflect.TypeFor[Setup]()},
		{"ResetURLRequest", reflect.TypeFor[ResetURLRequest]()},
		{"ResetURLResponse", reflect.TypeFor[ResetURLResponse]()},
		{"AllowedCIDRs", reflect.TypeFor[AllowedCIDRs]()},
		{"AuditActor", reflect.TypeFor[AuditActor]()},
		{"AuditEntry", reflect.TypeFor[AuditEntry]()},
		{"AuditList", reflect.TypeFor[AuditList]()},
	}
}
