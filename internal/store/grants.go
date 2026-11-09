package store

import (
	"context"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// GrantRow is one grant that applies to a user: from a group (GroupID set) or an org default.
// WorkspaceID is "" for a grant on every workspace (or an org-scoped resource).
type GrantRow struct {
	GroupID      string
	WorkspaceID  string
	ResourceType string
	Action       string
}

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
			if g.WorkspaceID == nil {
				continue
			}
			if !validID(*g.WorkspaceID) {
				return contract.NewError(contract.CodeValidationFailed, "request validation failed", "grants: unknown workspace "+*g.WorkspaceID)
			}
			if _, err := q.GetWorkspace(ctx, sqlc.GetWorkspaceParams{OrgID: orgID, ID: *g.WorkspaceID}); err != nil {
				if isNotFound(err) {
					return contract.NewError(contract.CodeValidationFailed, "request validation failed", "grants: unknown workspace "+*g.WorkspaceID)
				}
				return err
			}
		}
		if err := q.DeleteOrgDefaults(ctx, orgID); err != nil {
			return err
		}
		for _, g := range grants {
			ws := ""
			if g.WorkspaceID != nil {
				ws = *g.WorkspaceID
			}
			if err := q.InsertOrgDefault(ctx, sqlc.InsertOrgDefaultParams{OrgID: orgID, WorkspaceID: ws, ResourceType: g.ResourceType, Action: g.Action}); err != nil {
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
