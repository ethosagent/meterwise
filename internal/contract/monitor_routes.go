package contract

// monitorsUpdateMeta is the audit metadata of PATCH /api/workspaces/{id}/monitors: the monitors the request
// named and the mode each one has now. No thresholds and no workspace public id.
func monitorsUpdateMeta(in AuditInput) map[string]any {
	patch, _ := in.Req.(MonitorConfigPatch)
	cfg, _ := in.Resp.(MonitorConfig)
	names := []string{}
	modes := map[string]string{}
	for _, m := range patch.Touched() {
		names = append(names, string(m))
		modes[string(m)] = string(cfg.ModeOf(m))
	}
	return map[string]any{"monitors": names, "modes": modes}
}

// monitorFeedbackAudit audits a feedback change: which monitor and the new value (null when cleared).
func monitorFeedbackAudit() *AuditSpec {
	return &AuditSpec{
		Action: AuditMonitorEventFeedback, ResourceType: "monitor_event", ResourceID: pathID("eventId"), WorkspaceID: pathID("id"),
		Meta: func(in AuditInput) map[string]any {
			ev, _ := in.Resp.(MonitorEventView)
			return map[string]any{"monitor": string(ev.Monitor), "feedback": ev.Feedback}
		},
	}
}
