package store

import (
	"context"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// groupExists is CodeNotFound for an unknown (or malformed) group id.
func (s *Store) groupExists(ctx context.Context, orgID, id string) error {
	if !validID(id) {
		return errNotFound
	}
	found, err := s.q.GroupExists(ctx, sqlc.GroupExistsParams{OrgID: orgID, ID: id})
	if err != nil {
		return queryErr(err)
	}
	if !found {
		return errNotFound
	}
	return nil
}

// GroupGrants returns a group's grants (existence check, then a plain list); an unknown group is
// CodeNotFound.
func (s *Store) GroupGrants(ctx context.Context, orgID, groupID string) ([]contract.Grant, error) {
	if err := s.groupExists(ctx, orgID, groupID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListGroupGrants(ctx, sqlc.ListGroupGrantsParams{OrgID: orgID, GroupID: groupID})
	if err != nil {
		return nil, queryErr(err)
	}
	out := make([]contract.Grant, 0, len(rows))
	for _, r := range rows {
		out = append(out, contract.GrantRow{WorkspaceID: r.WorkspaceID, ResourceType: r.ResourceType, Action: r.Action}.ToGrant())
	}
	return out, nil
}

// ReplaceGroupGrants replaces all of a group's grants in one transaction. A grant on a workspace
// the group is not attached to is a validation error (422); the taxonomy was checked by the caller.
func (s *Store) ReplaceGroupGrants(ctx context.Context, orgID, groupID string, grants []contract.Grant) ([]contract.Grant, error) {
	err := s.withLockedGroup(ctx, orgID, groupID, func(q *sqlc.Queries) error {
		g, err := group(ctx, q, orgID, groupID)
		if err != nil {
			return err
		}
		attached := map[string]bool{}
		for _, w := range g.WorkspaceIDs {
			attached[w] = true
		}
		for _, gr := range grants {
			if gr.WorkspaceID != nil && !attached[*gr.WorkspaceID] {
				return contract.NewError(contract.CodeValidationFailed, "request validation failed", "grants: workspace "+*gr.WorkspaceID+" is not attached to the group")
			}
		}
		if err := q.DeleteGroupGrants(ctx, sqlc.DeleteGroupGrantsParams{OrgID: orgID, GroupID: groupID}); err != nil {
			return err
		}
		if len(grants) == 0 {
			return nil
		}
		cols := newGrantColumns(grants)
		return q.GrantManyToGroup(ctx, sqlc.GrantManyToGroupParams{
			OrgID: orgID, GroupID: groupID, WorkspaceIds: cols.workspaceIDs, ResourceTypes: cols.resourceTypes, Actions: cols.actions,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GroupGrants(ctx, orgID, groupID)
}

// grantColumns is a grant list as the parallel arrays the batch statements take; an org-wide grant
// has the empty string as its workspace id.
type grantColumns struct{ workspaceIDs, resourceTypes, actions []string }

func newGrantColumns(grants []contract.Grant) grantColumns {
	c := grantColumns{make([]string, len(grants)), make([]string, len(grants)), make([]string, len(grants))}
	for i, g := range grants {
		c.workspaceIDs[i], c.resourceTypes[i], c.actions[i] = g.WorkspaceOrEmpty(), g.ResourceType, g.Action
	}
	return c
}
