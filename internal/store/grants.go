package store

import (
	"context"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// GrantRow is one grant that applies to a user: from a group (GroupID set) or an org default.
// WorkspaceID is "" for a grant on every workspace (or an org-scoped resource).
type GrantRow = contract.GrantRow

// Grants returns every grant that applies to the user, in one round trip: the grants of the
// user's groups and the org defaults. It is the only read of authorization.
func (s *Store) Grants(ctx context.Context, orgID, userID string) ([]GrantRow, error) {
	if !validID(userID) {
		return nil, nil
	}
	rows, err := s.q.ListGrantsForUser(ctx, sqlc.ListGrantsForUserParams{OrgID: orgID, UserID: userID})
	if err != nil {
		return nil, queryErr(err)
	}
	out := make([]GrantRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, GrantRow{GroupID: r.GroupID, WorkspaceID: r.WorkspaceID, ResourceType: r.ResourceType, Action: r.Action})
	}
	return out, nil
}

// OrgDefaults returns the org default grants.
func (s *Store) OrgDefaults(ctx context.Context, orgID string) ([]contract.Grant, error) {
	rows, err := s.q.ListOrgDefaults(ctx, orgID)
	if err != nil {
		return nil, queryErr(err)
	}
	out := make([]contract.Grant, 0, len(rows))
	for _, r := range rows {
		out = append(out, grantOf(r.WorkspaceID, r.ResourceType, r.Action))
	}
	return out, nil
}

// ReplaceOrgDefaults replaces all org defaults in one transaction. A grant naming a workspace
// that is not in the org is a validation error (422); the taxonomy was checked by the caller.
func (s *Store) ReplaceOrgDefaults(ctx context.Context, orgID string, grants []contract.Grant) ([]contract.Grant, error) {
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		for _, g := range grants {
			if err := requireOrgWorkspace(ctx, q, orgID, g.WorkspaceID); err != nil {
				return err
			}
		}
		if err := q.DeleteOrgDefaults(ctx, orgID); err != nil {
			return err
		}
		for _, g := range grants {
			p := sqlc.InsertOrgDefaultParams{OrgID: orgID, WorkspaceID: derefID(g.WorkspaceID), ResourceType: g.ResourceType, Action: g.Action}
			if err := q.InsertOrgDefault(ctx, p); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, queryErr(err)
	}
	return s.OrgDefaults(ctx, orgID)
}

// derefID is the workspace id of a grant, "" for an org-wide one (how the queries spell NULL).
func derefID(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

// requireOrgWorkspace is nil for an org-wide grant or a workspace of the org, else a 422.
func requireOrgWorkspace(ctx context.Context, q *sqlc.Queries, orgID string, id *string) error {
	if id == nil {
		return nil
	}
	bad := contract.NewError(contract.CodeValidationFailed, "request validation failed", "grants: unknown workspace "+*id)
	if !validID(*id) {
		return bad
	}
	if _, err := q.GetWorkspace(ctx, sqlc.GetWorkspaceParams{OrgID: orgID, ID: *id}); err != nil {
		if isNotFound(err) {
			return bad
		}
		return err
	}
	return nil
}
