package access

import "github.com/ethosagent/meterwise/internal/contract"

// IsAdmin reports whether role is the admin role. Role knowledge lives here (and in contract and
// store) so no other package compares roles inline (archtest H11).
func IsAdmin(role contract.Role) bool { return role == contract.RoleAdmin }

// MayAssignRole is the escalation guard: an admin may hand out any role; anyone else (a member who
// holds member:manage) may hand out member only. It returns a 403 "admin required" otherwise.
func MayAssignRole(actor, target contract.Role) error {
	if actor == contract.RoleAdmin || target == contract.RoleMember {
		return nil
	}
	return contract.Forbidden("only an admin can assign the admin role", ReasonAdminRequired)
}
