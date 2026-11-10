package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

func toGroup(id, name string, created pgtype.Timestamptz, members int32, workspaces []string) contract.Group {
	if workspaces == nil {
		workspaces = []string{}
	}
	return contract.Group{ID: id, Name: name, MemberCount: int(members), WorkspaceIDs: workspaces, CreatedAt: rfc3339(created)}
}

// ListGroups returns the org's groups by name.
func (s *Store) ListGroups(ctx context.Context, orgID string) ([]contract.Group, error) {
	rows, err := s.q.ListGroups(ctx, orgID)
	if err != nil {
		return nil, queryErr(err)
	}
	out := make([]contract.Group, 0, len(rows))
	for _, r := range rows {
		out = append(out, toGroup(r.ID, r.Name, r.CreatedAt, r.MemberCount, r.WorkspaceIds))
	}
	return out, nil
}

// Group returns one group of the org; anything else is CodeNotFound.
func (s *Store) Group(ctx context.Context, orgID, id string) (contract.Group, error) {
	return group(ctx, s.q, orgID, id)
}

func group(ctx context.Context, q *sqlc.Queries, orgID, id string) (contract.Group, error) {
	if !validID(id) {
		return contract.Group{}, errNotFound
	}
	r, err := q.GetGroup(ctx, sqlc.GetGroupParams{OrgID: orgID, ID: id})
	if err != nil {
		return contract.Group{}, queryErr(err)
	}
	return toGroup(r.ID, r.Name, r.CreatedAt, r.MemberCount, r.WorkspaceIds), nil
}

func nameTaken() error {
	return contract.NewError(contract.CodeGroupNameTaken, "a group with that name already exists")
}

// CreateGroup inserts a group; a name used by another group (case-insensitively) is CodeGroupNameTaken.
func (s *Store) CreateGroup(ctx context.Context, orgID, name string) (contract.Group, error) {
	id, err := s.q.InsertGroup(ctx, sqlc.InsertGroupParams{OrgID: orgID, Name: name})
	if isUnique(err) {
		return contract.Group{}, nameTaken()
	}
	if err != nil {
		return contract.Group{}, queryErr(err)
	}
	return s.Group(ctx, orgID, id)
}

// withLockedGroup runs fn in one transaction holding the group's row lock. Every mutation of a
// group goes through it, so attach, detach, replace-grants, rename, delete and the member
// changes serialise on the group and none can race a delete into a foreign-key error. A
// malformed or unknown (or other org's) id is CodeNotFound before fn runs.
func (s *Store) withLockedGroup(ctx context.Context, orgID, id string, fn func(q *sqlc.Queries) error) error {
	if !validID(id) {
		return errNotFound
	}
	return queryErr(s.inTx(ctx, func(q *sqlc.Queries) error {
		if _, err := q.LockGroup(ctx, sqlc.LockGroupParams{OrgID: orgID, ID: id}); err != nil {
			return err
		}
		return fn(q)
	}))
}

// notFoundIfZero is the check after a write that must touch a row: a driver error passes through
// the scrubber, and no row affected is CodeNotFound.
func notFoundIfZero(n int64, err error) error {
	if err != nil {
		return queryErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

// RenameGroup renames a group; CodeNotFound or CodeGroupNameTaken.
func (s *Store) RenameGroup(ctx context.Context, orgID, id, name string) (contract.Group, error) {
	err := s.withLockedGroup(ctx, orgID, id, func(q *sqlc.Queries) error {
		n, err := q.RenameGroup(ctx, sqlc.RenameGroupParams{Name: name, OrgID: orgID, ID: id})
		if isUnique(err) {
			return nameTaken()
		}
		return notFoundIfZero(n, err)
	})
	if err != nil {
		return contract.Group{}, err
	}
	return s.Group(ctx, orgID, id)
}

// DeleteGroup deletes a group; its memberships, workspace links and grants go with it (cascade).
func (s *Store) DeleteGroup(ctx context.Context, orgID, id string) error {
	return s.withLockedGroup(ctx, orgID, id, func(q *sqlc.Queries) error {
		return notFoundIfZero(q.DeleteGroup(ctx, sqlc.DeleteGroupParams{OrgID: orgID, ID: id}))
	})
}

// AttachGroupWorkspace links a group to a workspace of the org and seeds one grant per spec on
// that workspace, in one transaction. Attaching again is a no-op that still returns the group.
func (s *Store) AttachGroupWorkspace(ctx context.Context, orgID, groupID, workspaceID string, seed []contract.GrantSpec) (contract.Group, error) {
	if !validID(workspaceID) {
		return contract.Group{}, errNotFound
	}
	var out contract.Group
	err := s.withLockedGroup(ctx, orgID, groupID, func(q *sqlc.Queries) error {
		if _, err := q.GetWorkspace(ctx, sqlc.GetWorkspaceParams{OrgID: orgID, ID: workspaceID}); err != nil {
			return err
		}
		linked, err := q.AttachGroupWorkspace(ctx, sqlc.AttachGroupWorkspaceParams{GroupID: groupID, WorkspaceID: workspaceID, OrgID: orgID})
		if err != nil {
			return err
		}
		if linked == 0 {
			seed = nil // already attached: nothing changes, whatever an admin removed stays removed
		}
		for _, g := range seed {
			if err := q.GrantToGroup(ctx, sqlc.GrantToGroupParams{GroupID: groupID, OrgID: orgID, WorkspaceID: workspaceID, ResourceType: g.ResourceType, Action: g.Action}); err != nil {
				return err
			}
		}
		out, err = group(ctx, q, orgID, groupID)
		return err
	})
	return out, err
}

// DetachGroupWorkspace unlinks a group from a workspace and drops every grant the group holds
// there, in one transaction. A group not attached to the workspace is CodeNotFound.
func (s *Store) DetachGroupWorkspace(ctx context.Context, orgID, groupID, workspaceID string) error {
	if !validID(workspaceID) {
		return errNotFound
	}
	return s.withLockedGroup(ctx, orgID, groupID, func(q *sqlc.Queries) error {
		n, err := q.DetachGroupWorkspace(ctx, sqlc.DetachGroupWorkspaceParams{OrgID: orgID, GroupID: groupID, WorkspaceID: workspaceID})
		if err := notFoundIfZero(n, err); err != nil {
			return err
		}
		return q.DeleteGroupGrantsOnWorkspace(ctx, sqlc.DeleteGroupGrantsOnWorkspaceParams{OrgID: orgID, GroupID: groupID, WorkspaceID: workspaceID})
	})
}
