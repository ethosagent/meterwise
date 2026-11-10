package contract

// memoryRoutes are the memory rows (plan 09). Every one is scoped to the workspace in the path.
func memoryRoutes() []Route {
	return []Route{
		{Method: "GET", Pattern: "/api/workspaces/{id}/memory", Permission: PermMonitorConfigRead, WorkspaceParam: "id", Summary: "The workspace's memory settings and whether memory is enabled (and why not)", Response: "MemorySettings"},
		{Method: "PATCH", Pattern: "/api/workspaces/{id}/memory", Permission: PermMonitorConfigWrite, WorkspaceParam: "id", Summary: "Change any subset of the memory settings; 422 names the field of a bad mode, threshold or limit", Request: "MemoryConfigPatch", Response: "MemorySettings", Audit: wsAuditSpec(AuditWorkspaceMemory, memoryUpdateMeta)},
		{Method: "GET", Pattern: "/api/workspaces/{id}/memory/stats", Permission: PermSavingsRead, WorkspaceParam: "id", Summary: "What memory did over range=7d|30d|custom: traces, hinted sessions, hits, feedback and the likely saving, which is never part of a certain total", Response: "MemoryStats"},
		{Method: "GET", Pattern: "/api/workspaces/{id}/traces", Permission: PermSessionRead, WorkspaceParam: "id", Summary: "The workspace's remembered traces, newest first, paged with the opaque cursor and limit (1-100)", Response: "TraceList"},
		{Method: "GET", Pattern: "/api/workspaces/{id}/traces/{traceId}", Permission: PermSessionRead, WorkspaceParam: "id", Summary: "One trace with its observations, signature and the sessions it helped; 404 when it is not in the workspace", Response: "TraceDetail"},
		{Method: "DELETE", Pattern: "/api/workspaces/{id}/traces", Permission: PermWorkspaceManage, WorkspaceParam: "id", Summary: "Forget every trace of the workspace; 204, audited with the number removed", Response: "ForgetResult", Status: 204, Audit: memoryForgetAudit()},
		{Method: "POST", Pattern: "/api/workspaces/{id}/memory-hits/{hitId}/feedback", Permission: PermSessionRead, WorkspaceParam: "id", Summary: "Mark a memory hint helpful or irrelevant, or clear it with null; 404 when the hit is not in the workspace", Request: "MemoryHitFeedbackRequest", Response: "MemoryHitView", Audit: memoryHitFeedbackAudit()},
	}
}

// memoryUpdateMeta is the audit metadata of PATCH .../memory: the fields named and the mode now. No model names.
func memoryUpdateMeta(in AuditInput) map[string]any {
	patch, _ := in.Req.(MemoryConfigPatch)
	set, _ := in.Resp.(MemorySettings)
	return map[string]any{"fields": patch.Touched(), "mode": string(set.Config.Mode)}
}

func memoryForgetAudit() *AuditSpec {
	return &AuditSpec{
		Action: AuditMemoryForget, ResourceType: "workspace", ResourceID: pathID("id"), WorkspaceID: pathID("id"),
		Meta: func(in AuditInput) map[string]any {
			r, _ := in.Resp.(ForgetResult)
			return map[string]any{"count": r.Count}
		},
	}
}

func memoryHitFeedbackAudit() *AuditSpec {
	return &AuditSpec{
		Action: AuditMemoryHitFeedback, ResourceType: "memory_hit", ResourceID: pathID("hitId"), WorkspaceID: pathID("id"),
		Meta: func(in AuditInput) map[string]any {
			h, _ := in.Resp.(MemoryHitView)
			return map[string]any{"feedback": h.Feedback}
		},
	}
}
