package store

import (
	"context"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

func toGroup(id, name string, created time.Time, members int32, workspaces []string) contract.Group {
	if workspaces == nil {
		workspaces = []string{}
	}
	return contract.Group{ID: id, Name: name, MemberCount: int(members), WorkspaceIDs: workspaces, CreatedAt: created.UTC().Format(time.RFC3339)}
}

// ListGroups returns the org's groups by name.
func (s *Store) ListGroups(ctx context.Context, orgID string) ([]contract.Group, error) {
	rows, err := s.q.ListGroups(ctx, orgID)
	if err != nil {
		return nil, queryErr(err)
	}
	out := make([]contract.Group, 0, len(rows))
	for _, r := range rows {
		out = append(out, toGroup(r.ID, r.Name, ts(r.CreatedAt), r.MemberCount, r.WorkspaceIds))
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
	return toGroup(r.ID, r.Name, ts(r.CreatedAt), r.MemberCount, r.WorkspaceIds), nil
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

// RenameGroup renames a group; CodeNotFound or CodeGroupNameTaken.
func (s *Store) RenameGroup(ctx context.Context, orgID, id, name string) (contract.Group, error) {
	if !validID(id) {
		return contract.Group{}, errNotFound
	}
	n, err := s.q.RenameGroup(ctx, sqlc.RenameGroupParams{Name: name, OrgID: orgID, ID: id})
	if isUnique(err) {
		return contract.Group{}, nameTaken()
	}
	if err != nil {
		return contract.Group{}, queryErr(err)
	}
	if n == 0 {
		return contract.Group{}, errNotFound
	}
	return s.Group(ctx, orgID, id)
}

// DeleteGroup deletes a group; its memberships, workspace links and grants go with it (cascade).
func (s *Store) DeleteGroup(ctx context.Context, orgID, id string) error {
	if !validID(id) {
		return errNotFound
	}
	n, err := s.q.DeleteGroup(ctx, sqlc.DeleteGroupParams{OrgID: orgID, ID: id})
	if err != nil {
		return queryErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

// GrantSpec is a resource type and action; the taxonomy (package access) decides which exist.
type GrantSpec struct{ ResourceType, Action string }

// AttachGroupWorkspace links a group to a workspace of the org and seeds one grant per spec on
// that workspace, in one transaction. Attaching again is a no-op that still returns the group.
func (s *Store) AttachGroupWorkspace(ctx context.Context, orgID, groupID, workspaceID string, seed []GrantSpec) (contract.Group, error) {
	if !validID(groupID) || !validID(workspaceID) {
		return contract.Group{}, errNotFound
	}
	var out contract.Group
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		if _, err := group(ctx, q, orgID, groupID); err != nil {
			return err
		}
		if _, err := q.GetWorkspace(ctx, sqlc.GetWorkspaceParams{OrgID: orgID, ID: workspaceID}); err != nil {
			return err
		}
		if err := q.AttachGroupWorkspace(ctx, sqlc.AttachGroupWorkspaceParams{GroupID: groupID, WorkspaceID: workspaceID, OrgID: orgID}); err != nil {
			return err
		}
		for _, g := range seed {
			if err := q.GrantToGroup(ctx, sqlc.GrantToGroupParams{GroupID: groupID, OrgID: orgID, WorkspaceID: workspaceID, ResourceType: g.ResourceType, Action: g.Action}); err != nil {
				return err
			}
		}
		var err error
		out, err = group(ctx, q, orgID, groupID)
		return err
	})
	return out, queryErr(err)
}

// DetachGroupWorkspace unlinks a group from a workspace and drops every grant the group holds
// there, in one transaction. A group not attached to the workspace is CodeNotFound.
func (s *Store) DetachGroupWorkspace(ctx context.Context, orgID, groupID, workspaceID string) error {
	if !validID(groupID) || !validID(workspaceID) {
		return errNotFound
	}
	return queryErr(s.inTx(ctx, func(q *sqlc.Queries) error {
		n, err := q.DetachGroupWorkspace(ctx, sqlc.DetachGroupWorkspaceParams{OrgID: orgID, GroupID: groupID, WorkspaceID: workspaceID})
		if err != nil {
			return err
		}
		if n == 0 {
			return errNotFound
		}
		return q.DeleteGroupGrantsOnWorkspace(ctx, sqlc.DeleteGroupGrantsOnWorkspaceParams{OrgID: orgID, GroupID: groupID, WorkspaceID: workspaceID})
	}))
}

func grantOf(workspaceID, resourceType, action string) contract.Grant {
	g := contract.Grant{ResourceType: resourceType, Action: action}
	if workspaceID != "" {
		g.WorkspaceID = &workspaceID
	}
	return g
}

// GroupGrants returns a group's grants; an unknown group is CodeNotFound.
func (s *Store) GroupGrants(ctx context.Context, orgID, groupID string) ([]contract.Grant, error) {
	if _, err := s.Group(ctx, orgID, groupID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListGroupGrants(ctx, sqlc.ListGroupGrantsParams{OrgID: orgID, GroupID: groupID})
	if err != nil {
		return nil, queryErr(err)
	}
	out := make([]contract.Grant, 0, len(rows))
	for _, r := range rows {
		out = append(out, grantOf(r.WorkspaceID, r.ResourceType, r.Action))
	}
	return out, nil
}

// ReplaceGroupGrants replaces all of a group's grants in one transaction. A grant on a workspace
// the group is not attached to is a validation error (422); the taxonomy was checked by the caller.
func (s *Store) ReplaceGroupGrants(ctx context.Context, orgID, groupID string, grants []contract.Grant) ([]contract.Grant, error) {
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
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
		for _, gr := range grants {
			ws := ""
			if gr.WorkspaceID != nil {
				ws = *gr.WorkspaceID
			}
			if err := q.GrantToGroup(ctx, sqlc.GrantToGroupParams{GroupID: groupID, OrgID: orgID, WorkspaceID: ws, ResourceType: gr.ResourceType, Action: gr.Action}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, queryErr(err)
	}
	return s.GroupGrants(ctx, orgID, groupID)
}

// GroupMembers lists a group's active members; an unknown group is CodeNotFound.
func (s *Store) GroupMembers(ctx context.Context, orgID, groupID string) ([]contract.Member, error) {
	if _, err := s.Group(ctx, orgID, groupID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListGroupMembers(ctx, sqlc.ListGroupMembersParams{OrgID: orgID, GroupID: groupID})
	if err != nil {
		return nil, queryErr(err)
	}
	out := make([]contract.Member, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMember(r.UserID, r.Email, r.Name, r.AvatarUrl, r.Role, r.JoinedAt, r.GroupIds))
	}
	return out, nil
}

// AddGroupMember puts an active member in a group; it touches group_users only. A user who is
// not an active member is a validation error (422); adding twice is a no-op.
func (s *Store) AddGroupMember(ctx context.Context, orgID, groupID, userID string) error {
	if !validID(userID) {
		return contract.NewError(contract.CodeValidationFailed, "request validation failed", "userId: not an active member")
	}
	return queryErr(s.inTx(ctx, func(q *sqlc.Queries) error {
		if _, err := group(ctx, q, orgID, groupID); err != nil {
			return err
		}
		if _, err := q.LockActiveMembershipShared(ctx, sqlc.LockActiveMembershipSharedParams{OrgID: orgID, UserID: userID}); err != nil {
			if isNotFound(err) {
				return contract.NewError(contract.CodeValidationFailed, "request validation failed", "userId: not an active member")
			}
			return err
		}
		return q.AddGroupUser(ctx, sqlc.AddGroupUserParams{GroupID: groupID, UserID: userID, OrgID: orgID})
	}))
}

// RemoveGroupMember takes a user out of a group; a user not in it (or an unknown group) is CodeNotFound.
func (s *Store) RemoveGroupMember(ctx context.Context, orgID, groupID, userID string) error {
	if !validID(groupID) || !validID(userID) {
		return errNotFound
	}
	n, err := s.q.RemoveGroupUser(ctx, sqlc.RemoveGroupUserParams{OrgID: orgID, GroupID: groupID, UserID: userID})
	if err != nil {
		return queryErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}
