package access

import (
	"sort"
	"strings"

	"github.com/ethosagent/meterwise/internal/contract"
)

// The reasons a Result carries (403 bodies and, later, audit rows).
const (
	ReasonAdmin         = "admin"
	ReasonMember        = "member"
	ReasonSelf          = "self"
	ReasonOrgDefault    = "org-default"
	ReasonNoGrant       = "no matching grant"
	ReasonAdminRequired = "admin required"
	groupReasonPrefix   = "group:"
)

// Grant is one granted permission. GroupID is empty for an org default; WorkspaceID is empty for
// "every workspace" (and for org-scoped resources).
type Grant = contract.GrantRow

// Subject is who asks.
type Subject struct {
	UserID string
	Role   contract.Role
}

// Request is one question: may Subject do Permission, on WorkspaceID when the permission is
// workspace-scoped?
type Request struct {
	Subject     Subject
	Permission  contract.Permission
	WorkspaceID string
}

// Result is the whole answer, not a bool, so a machine principal could later intersect it
// with scopes.
type Result struct {
	Allowed bool
	Reason  string
}

// PermissionEvaluator decides a Request from the grants of its subject. It is pure: no I/O, no clock.
//
// Rules: an admin is allowed everything. A member is allowed PermMember routes; PermAdmin is for
// admins only; a taxonomy permission needs a matching grant, the union of the member's group
// grants and the org defaults. There is no deny.
type PermissionEvaluator struct{}

// NeedsGrants reports whether Evaluate for req can depend on grants, so the caller loads them
// only when they matter (an admin, PermSelf, PermMember and PermAdmin never need them).
func (PermissionEvaluator) NeedsGrants(req Request) bool {
	if req.Subject.Role == contract.RoleAdmin {
		return false
	}
	switch req.Permission {
	case contract.PermSelf, contract.PermMember, contract.PermAdmin:
		return false
	}
	return req.Subject.Role == contract.RoleMember
}

// Evaluate answers req. Anything it does not understand (an unknown role, an unknown permission,
// a workspace-scoped permission with no workspace) is denied.
func (PermissionEvaluator) Evaluate(req Request, grants []Grant) Result {
	role := req.Subject.Role
	switch {
	case req.Permission == contract.PermSelf:
		return Result{true, ReasonSelf}
	case !role.Valid():
		return Result{false, ReasonNoGrant}
	case role == contract.RoleAdmin:
		return Result{true, ReasonAdmin}
	case req.Permission == contract.PermMember:
		return Result{true, ReasonMember}
	case req.Permission == contract.PermAdmin:
		return Result{false, ReasonAdminRequired}
	}
	rt, action, ok := Parse(req.Permission)
	if !ok {
		return Result{false, ReasonNoGrant}
	}
	scope, _ := ScopeOf(rt)
	if scope == ScopeWorkspace && req.WorkspaceID == "" {
		return Result{false, ReasonNoGrant}
	}
	var groups []string
	orgDefault := false
	for _, g := range grants {
		if g.ResourceType != rt || g.Action != action || !covers(scope, g.WorkspaceID, req.WorkspaceID) {
			continue
		}
		if g.GroupID == "" {
			orgDefault = true
		} else {
			groups = append(groups, g.GroupID)
		}
	}
	if len(groups) > 0 {
		sort.Strings(groups)
		return Result{true, groupReasonPrefix + groups[0]}
	}
	if orgDefault {
		return Result{true, ReasonOrgDefault}
	}
	return Result{false, ReasonNoGrant}
}

// covers reports whether a grant on grantWS applies to the workspace asked about. An org-scoped
// resource is matched only by a grant with no workspace; a workspace-scoped one by a grant on that
// workspace or on every workspace (empty).
func covers(scope Scope, grantWS, want string) bool {
	if scope == ScopeOrg {
		return grantWS == ""
	}
	return grantWS == "" || grantWS == want
}

// Snapshot flattens a member's grants for GET /api/me/permissions: an admin is {admin: true} with
// no grants; anyone else gets the sorted, de-duplicated union of group grants and org defaults.
func (PermissionEvaluator) Snapshot(subject Subject, grants []Grant) contract.PermissionSnapshot {
	if subject.Role == contract.RoleAdmin {
		return contract.PermissionSnapshot{Admin: true, Grants: []contract.Grant{}}
	}
	seen := map[string]bool{}
	out := []contract.Grant{}
	for _, g := range grants {
		key := g.ResourceType + "\x00" + g.Action + "\x00" + g.WorkspaceID
		if seen[key] || !Valid(g.ResourceType, g.Action) {
			continue
		}
		seen[key] = true
		c := contract.Grant{ResourceType: g.ResourceType, Action: g.Action}
		if g.WorkspaceID != "" {
			ws := g.WorkspaceID
			c.WorkspaceID = &ws
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.ResourceType != b.ResourceType {
			return a.ResourceType < b.ResourceType
		}
		if a.Action != b.Action {
			return a.Action < b.Action
		}
		return strings.Compare(deref(a.WorkspaceID), deref(b.WorkspaceID)) < 0
	})
	return contract.PermissionSnapshot{Grants: out}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
