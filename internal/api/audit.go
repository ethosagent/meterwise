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

// emitAudit records what the caller just did, as the route row declares. It runs after the change
// committed; a failure is logged at ERROR by audit.Emit and never fails the request. Metadata is
// small and never carries a workspace public id or URL.
func (s *server) emitAudit(c Call, spec *contract.AuditSpec, req, resp any) {
	in := contract.AuditInput{Path: c.Path, Principal: c.Principal, Req: req, Resp: resp}
	e := contract.AuditEvent{
		OrgID: c.Principal.OrgID, ActorID: c.Principal.UserID, Action: spec.Action,
		ResourceType: spec.ResourceType, ResourceID: spec.ResourceID(in), IP: c.IP,
	}
	if spec.WorkspaceID != nil {
		e.WorkspaceID = spec.WorkspaceID(in)
	}
	if spec.Meta != nil {
		e.Metadata = spec.Meta(in)
	}
	audit.Emit(c.Ctx, s.Log, s.Audit, e)
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
			ID: strconv.FormatInt(r.ID, 10), At: contract.RFC3339(r.At), Action: r.Action, ResourceType: r.ResourceType,
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
