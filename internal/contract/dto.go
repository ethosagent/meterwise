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
	ActiveWorkspaceID string      `json:"activeWorkspaceId"`
	Preferences       Preferences `json:"preferences"`
}

// WorkspaceView is the Workspace API type: ProxyURL is
// https://{proxyPublicHost}/w/{publicId}.
type WorkspaceView struct {
	ID        string         `json:"id"`
	PublicID  string         `json:"publicId"`
	Name      string         `json:"name"`
	IsDefault bool           `json:"isDefault"`
	ProxyURL  string         `json:"proxyUrl"`
	Settings  map[string]any `json:"settings"`
	CreatedAt string         `json:"createdAt"`
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
	}
}
