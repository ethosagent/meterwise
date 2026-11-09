package api

// registerTenancy wires the Plan 02 routes. Authorization is the gateway's, by the route table.
func (s *server) registerTenancy() {
	register(s, "GET", "/api/me/preferences", s.getPreferences)
	register(s, "PATCH", "/api/me/preferences", s.patchPreferences)
	register(s, "GET", "/api/me", s.getMe)
	register(s, "GET", "/api/me/permissions", s.mePermissions)
	register(s, "POST", "/api/auth/complete-signup", s.completeSignup)
	register(s, "GET", "/api/workspaces", s.listWorkspaces)
	register(s, "POST", "/api/workspaces", s.createWorkspace)
	register(s, "PATCH", "/api/workspaces/{id}", s.updateWorkspace)
	register(s, "DELETE", "/api/workspaces/{id}", s.deleteWorkspace)
	register(s, "POST", "/api/workspaces/{id}/switch", s.switchWorkspace)
	register(s, "GET", "/api/invitations", s.listInvitations)
	register(s, "POST", "/api/invitations", s.createInvitation)
	register(s, "POST", "/api/invitations/{id}/resend", s.resendInvitation)
	register(s, "DELETE", "/api/invitations/{id}", s.revokeInvitation)
}

// registerAccess wires the Plan 03 routes.
func (s *server) registerAccess() {
	register(s, "GET", "/api/members", s.listMembers)
	register(s, "PATCH", "/api/members/{userId}", s.updateMember)
	register(s, "DELETE", "/api/members/{userId}", s.removeMember)
	register(s, "GET", "/api/groups", s.listGroups)
	register(s, "POST", "/api/groups", s.createGroup)
	register(s, "GET", "/api/groups/{id}", s.getGroup)
	register(s, "PATCH", "/api/groups/{id}", s.renameGroup)
	register(s, "DELETE", "/api/groups/{id}", s.deleteGroup)
	register(s, "GET", "/api/groups/{id}/permissions", s.groupGrants)
	register(s, "PUT", "/api/groups/{id}/permissions", s.setGroupGrants)
	register(s, "POST", "/api/groups/{id}/workspaces", s.attachWorkspace)
	register(s, "DELETE", "/api/groups/{id}/workspaces/{wsId}", s.detachWorkspace)
	register(s, "GET", "/api/groups/{id}/members", s.groupMembers)
	register(s, "POST", "/api/groups/{id}/members", s.addGroupMember)
	register(s, "PUT", "/api/groups/{id}/members", s.setGroupMembers)
	register(s, "DELETE", "/api/groups/{id}/members/{userId}", s.removeGroupMember)
	register(s, "GET", "/api/organization/permissions", s.orgDefaults)
	register(s, "PUT", "/api/organization/permissions", s.setOrgDefaults)
}

// registerWorkspaceURL wires the Plan 04 routes: the setup page, the URL reset and its guard, and
// the audit log.
func (s *server) registerWorkspaceURL() {
	register(s, "GET", "/api/workspaces/{id}/setup", s.workspaceSetup)
	register(s, "POST", "/api/workspaces/{id}/url/reset", s.resetWorkspaceURL)
	register(s, "POST", "/api/workspaces/{id}/url/retire-previous", s.retirePreviousURL)
	register(s, "PUT", "/api/workspaces/{id}/url/allowed-cidrs", s.setAllowedCIDRs)
	register(s, "GET", "/api/audit-log", s.listAuditLog)
}
