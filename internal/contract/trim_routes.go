package contract

// trimRoutes are the trim rows (plan 11). Every one is scoped to the workspace in the path.
func trimRoutes() []Route {
	return []Route{
		{Method: "GET", Pattern: "/api/workspaces/{id}/trim", Permission: PermMonitorConfigRead, WorkspaceParam: "id", Summary: "The workspace's trim settings with defaults filled in", Response: "TrimConfig"},
		{Method: "PATCH", Pattern: "/api/workspaces/{id}/trim", Permission: PermMonitorConfigWrite, WorkspaceParam: "id", Summary: "Change any subset of the trim settings; 422 names the field of a bad mode, limit or stage", Request: "TrimConfigPatch", Response: "TrimConfig", Audit: wsAuditSpec(AuditWorkspaceTrim, trimUpdateMeta)},
		{Method: "GET", Pattern: "/api/workspaces/{id}/trim/stats", Permission: PermSavingsRead, WorkspaceParam: "id", Summary: "What trimming did over range=7d|30d|custom: calls, trimmed calls, tokens, saved and would-save money, per stage and per tool; saved, likely and would-save are never summed", Response: "TrimStats"},
		{Method: "GET", Pattern: "/api/workspaces/{id}/calls/{callId}/trim", Permission: PermSessionRead, WorkspaceParam: "id", Summary: "What trimming did to one call: sizes and markers only, never message text; 404 when the call is not in the workspace", Response: "TrimPreview"},
	}
}

// trimUpdateMeta is the audit metadata of PATCH .../trim: the fields the request named and the mode now.
func trimUpdateMeta(in AuditInput) map[string]any {
	patch, _ := in.Req.(TrimConfigPatch)
	cfg, _ := in.Resp.(TrimConfig)
	return map[string]any{"fields": patch.Touched(), "mode": string(cfg.Mode)}
}
