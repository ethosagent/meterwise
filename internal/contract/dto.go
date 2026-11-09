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
	}
}
