package api

import (
	"context"
	"strconv"

	"github.com/ethosagent/meterwise/internal/audit"
	"github.com/ethosagent/meterwise/internal/contract"
)

// AuditLog is what the control plane needs from the audit package: append (after a change has
// committed) and list. *audit.Sink implements it.
type AuditLog interface {
	contract.AuditSink
	List(ctx context.Context, q contract.AuditQuery) (contract.AuditPage, error)
}

// audit records what the caller just did. It runs after the change committed; a failure is logged
// at ERROR by audit.Emit and never fails the request. Metadata is small and never carries a
// workspace public id or URL.
func (s *server) audit(c Call, action contract.AuditAction, resourceType, resourceID, workspaceID string, meta map[string]any) {
	audit.Emit(c.Ctx, s.Log, s.Audit, contract.AuditEvent{
		OrgID: c.Principal.OrgID, WorkspaceID: workspaceID, ActorID: c.Principal.UserID, Action: action,
		ResourceType: resourceType, ResourceID: resourceID, IP: c.IP, Metadata: meta,
	})
}

func (s *server) listAuditLog(c Call, _ none) (contract.AuditList, error) {
	q := contract.AuditQuery{Action: contract.AuditAction(c.Query("action")), WorkspaceID: c.Query("workspaceId"), Cursor: c.Query("cursor")}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return contract.AuditList{}, contract.Invalid("limit", "must be a whole number from 1 to 200")
		}
		q.Limit = n
	}
	page, err := s.Audit.List(c.Ctx, q)
	if err != nil {
		return contract.AuditList{}, err
	}
	out := contract.AuditList{Items: make([]contract.AuditEntry, 0, len(page.Items)), NextCursor: strOrNil(page.NextCursor)}
	for _, r := range page.Items {
		e := contract.AuditEntry{
			ID: strconv.FormatInt(r.ID, 10), At: rfc3339(r.At), Action: r.Action, ResourceType: r.ResourceType,
			ResourceID: strOrNil(r.ResourceID), WorkspaceID: strOrNil(r.WorkspaceID), IP: strOrNil(r.IP), Metadata: r.Metadata,
		}
		if e.Metadata == nil {
			e.Metadata = map[string]any{}
		}
		if r.ActorID != "" {
			e.Actor = &contract.AuditActor{UserID: r.ActorID, Email: r.ActorEmail, Name: r.ActorName}
		}
		out.Items = append(out.Items, e)
	}
	return out, nil
}

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
