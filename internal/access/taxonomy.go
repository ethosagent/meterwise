package access

import (
	"sort"
	"strings"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Scope says what a resource type's grants are attached to.
type Scope string

// The two scopes. A workspace-scoped grant names a workspace (or, in the org defaults, every
// workspace); an org-scoped grant never names one.
const (
	ScopeWorkspace Scope = "workspace"
	ScopeOrg       Scope = "org"
)

// Resource is one row of the taxonomy.
type Resource struct {
	Type    string
	Scope   Scope
	Actions []string
}

// taxonomy is the sole definition of resource types and actions (Plan 03 section 4). It is a code
// change, not a table; web/src/access/taxonomy.ts is generated from it by cmd/gen.
var taxonomy = map[string]Resource{
	"workspace":      {"workspace", ScopeWorkspace, []string{"read", "write", "manage"}},
	"workspace_url":  {"workspace_url", ScopeWorkspace, []string{"read", "manage"}},
	"session":        {"session", ScopeWorkspace, []string{"read"}},
	"monitor_config": {"monitor_config", ScopeWorkspace, []string{"read", "write"}},
	"savings":        {"savings", ScopeWorkspace, []string{"read"}},
	"member":         {"member", ScopeOrg, []string{"read", "manage"}},
	"audit_log":      {"audit_log", ScopeOrg, []string{"read"}},
}

// Taxonomy returns every resource type sorted by name; the slices are copies.
func Taxonomy() []Resource {
	out := make([]Resource, 0, len(taxonomy))
	for _, r := range taxonomy {
		r.Actions = append([]string(nil), r.Actions...)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// ScopeOf returns the scope of a resource type; ok is false for an unknown one.
func ScopeOf(resourceType string) (Scope, bool) {
	r, ok := taxonomy[resourceType]
	return r.Scope, ok
}

// Valid reports whether resourceType:action is in the taxonomy.
func Valid(resourceType, action string) bool {
	r, ok := taxonomy[resourceType]
	if !ok {
		return false
	}
	for _, a := range r.Actions {
		if a == action {
			return true
		}
	}
	return false
}

// Parse splits "resource:action" and reports whether the pair is in the taxonomy.
func Parse(p contract.Permission) (resourceType, action string, ok bool) {
	resourceType, action, found := strings.Cut(string(p), ":")
	if !found || !Valid(resourceType, action) {
		return "", "", false
	}
	return resourceType, action, true
}

// WorkspaceReadSeed is what attaching a group to a workspace seeds: the read action of every
// workspace-scoped resource type, in taxonomy order.
func WorkspaceReadSeed() []Grant {
	var out []Grant
	for _, r := range Taxonomy() {
		if r.Scope == ScopeWorkspace && Valid(r.Type, "read") {
			out = append(out, Grant{ResourceType: r.Type, Action: "read"})
		}
	}
	return out
}
