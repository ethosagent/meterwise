package contract

// routerRoutes are the router rows (plan 10). Every one is scoped to the workspace in the path.
func routerRoutes() []Route {
	return []Route{
		{Method: "GET", Pattern: "/api/workspaces/{id}/router", Permission: PermMonitorConfigRead, WorkspaceParam: "id", Summary: "The workspace's router settings with the default, suggested and effective model pairs", Response: "RouterSettings"},
		{Method: "PATCH", Pattern: "/api/workspaces/{id}/router", Permission: PermMonitorConfigWrite, WorkspaceParam: "id", Summary: "Change any subset of the router settings; 422 names the field of a bad mode, threshold or pair", Request: "RouterConfigPatch", Response: "RouterSettings", Audit: wsAuditSpec(AuditWorkspaceRouter, routerUpdateMeta)},
		{Method: "GET", Pattern: "/api/workspaces/{id}/router/stats", Permission: PermSavingsRead, WorkspaceParam: "id", Summary: "What routing did over range=7d|30d|custom: routed and observed calls, saved and would-save money, guardrail and per-pair numbers; saved and would-save are never summed", Response: "RouterStats"},
		{Method: "POST", Pattern: "/api/workspaces/{id}/calls/{callId}/router-feedback", Permission: PermSessionRead, WorkspaceParam: "id", Summary: "Mark a routed call worse or fine, or clear it with null; 404 when the call is not in the workspace", Request: "RouterFeedbackRequest", Response: "RouterFeedbackView", Audit: routerFeedbackAudit()},
	}
}

// routerUpdateMeta is the audit metadata of PATCH .../router: the fields named, the mode now and the number of
// pairs in the workspace's own map. No model names.
func routerUpdateMeta(in AuditInput) map[string]any {
	patch, _ := in.Req.(RouterConfigPatch)
	set, _ := in.Resp.(RouterSettings)
	return map[string]any{"fields": patch.Touched(), "mode": string(set.Config.Mode), "pairs": len(set.Config.Map)}
}

func routerFeedbackAudit() *AuditSpec {
	return &AuditSpec{
		Action: AuditRouterFeedback, ResourceType: "call", ResourceID: pathID("callId"), WorkspaceID: pathID("id"),
		Meta: func(in AuditInput) map[string]any {
			v, _ := in.Resp.(RouterFeedbackView)
			return map[string]any{"verdict": v.Verdict}
		},
	}
}
