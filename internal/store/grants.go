package store

import (
	"context"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// Grants returns every grant that applies to the user, in one round trip: the grants of the
// user's groups and the org defaults. It is the only read of authorization.
func (s *Store) Grants(ctx context.Context, orgID, userID string) ([]contract.GrantRow, error) {
	if !validID(userID) {
		return nil, nil
	}
	rows, err := s.q.ListGrantsForUser(ctx, sqlc.ListGrantsForUserParams{OrgID: orgID, UserID: userID})
	if err != nil {
		return nil, queryErr(err)
	}
	out := make([]contract.GrantRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, contract.GrantRow{GroupID: r.GroupID, WorkspaceID: r.WorkspaceID, ResourceType: r.ResourceType, Action: r.Action})
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
		out = append(out, contract.GrantRow{WorkspaceID: r.WorkspaceID, ResourceType: r.ResourceType, Action: r.Action}.ToGrant())
	}
	return out, nil
}

// ReplaceOrgDefaults replaces all org defaults in one transaction. A grant naming a workspace
// that is not in the org is a validation error (422); the taxonomy was checked by the caller.
func (s *Store) ReplaceOrgDefaults(ctx context.Context, orgID string, grants []contract.Grant) ([]contract.Grant, error) {
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := requireOrgWorkspaces(ctx, q, orgID, grants); err != nil {
			return err
		}
		if err := q.DeleteOrgDefaults(ctx, orgID); err != nil {
			return err
		}
		if len(grants) == 0 {
			return nil
		}
		cols := newGrantColumns(grants)
		return q.InsertOrgDefaults(ctx, sqlc.InsertOrgDefaultsParams{
			OrgID: orgID, WorkspaceIds: cols.workspaceIDs, ResourceTypes: cols.resourceTypes, Actions: cols.actions,
		})
	})
	if err != nil {
		return nil, queryErr(err)
	}
	return s.OrgDefaults(ctx, orgID)
}

// requireOrgWorkspaces is nil when every workspace the grants name belongs to the org, else a 422
// naming the first that does not. It reads once, whatever the list's length.
func requireOrgWorkspaces(ctx context.Context, q *sqlc.Queries, orgID string, grants []contract.Grant) error {
	var ids []string
	distinct := map[string]bool{}
	for _, g := range grants {
		if g.WorkspaceID == nil {
			continue
		}
		id := *g.WorkspaceID
		if !validID(id) {
			return unknownWorkspace(id)
		}
		if !distinct[id] {
			distinct[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	n, err := q.CountOrgWorkspacesAmong(ctx, sqlc.CountOrgWorkspacesAmongParams{OrgID: orgID, Ids: ids})
	if err != nil {
		return err
	}
	if int(n) == len(ids) {
		return nil
	}
	for _, id := range ids { // some are missing: name the first, so the message stays as it was
		if _, err := q.GetWorkspace(ctx, sqlc.GetWorkspaceParams{OrgID: orgID, ID: id}); err != nil {
			if isNotFound(err) {
				return unknownWorkspace(id)
			}
			return err
		}
	}
	return nil
}

func unknownWorkspace(id string) error {
	return contract.NewError(contract.CodeValidationFailed, "request validation failed", "grants: unknown workspace "+id)
}
