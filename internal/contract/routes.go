package contract

import "sort"

// Permission names what a caller needs for a route. The empty Permission with
// Public set means no credential is required; any other route must name one.
type Permission string

// PermSelf is held by every authenticated principal and covers the caller's own
// profile data only; Plan 03 maps it into the RBAC taxonomy.
const PermSelf Permission = "self"

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
	Response   string     `json:"response"`          // DTO name from DTOs()
}

var routes = []Route{
	{Method: "GET", Pattern: "/healthz", Public: true, Summary: "Liveness probe", Response: "Health"},
	{Method: "GET", Pattern: "/api/config", Public: true, Summary: "App name and public proxy host for the shell", Response: "AppConfig"},
	{Method: "GET", Pattern: "/api/me/preferences", Permission: PermSelf, Summary: "Read the caller's UI preferences", Response: "Preferences"},
	{Method: "PATCH", Pattern: "/api/me/preferences", Permission: PermSelf, Summary: "Change the caller's UI preferences", Request: "PreferencesPatch", Response: "Preferences"},
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
