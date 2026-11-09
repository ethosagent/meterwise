package store

import (
	"context"
	"strings"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// GroupMembers lists a group's active members in one read; an unknown group is CodeNotFound.
func (s *Store) GroupMembers(ctx context.Context, orgID, groupID string) ([]contract.Member, error) {
	if !validID(groupID) {
		return nil, errNotFound
	}
	rows, err := s.q.ListGroupMembers(ctx, sqlc.ListGroupMembersParams{OrgID: orgID, GroupID: groupID})
	if err != nil {
		return nil, queryErr(err)
	}
	if len(rows) == 0 {
		return nil, errNotFound // a group that exists always yields at least one row
	}
	out := make([]contract.Member, 0, len(rows))
	for _, r := range rows {
		if r.UserID != "" { // the placeholder row of a group with no active member
			out = append(out, toMember(r.UserID, r.Email, r.Name, r.AvatarUrl, r.Role, r.JoinedAt, r.GroupIds))
		}
	}
	return out, nil
}

// AddGroupMember puts an active member in a group; it touches group_users only. A user who is
// not an active member is a validation error (422); adding twice is a no-op.
func (s *Store) AddGroupMember(ctx context.Context, orgID, groupID, userID string) error {
	notMember := contract.NewError(contract.CodeValidationFailed, "request validation failed", "userId: not an active member")
	if !validID(userID) {
		return notMember
	}
	return s.withLockedGroup(ctx, orgID, groupID, func(q *sqlc.Queries) error {
		if _, err := q.LockActiveMembershipShared(ctx, sqlc.LockActiveMembershipSharedParams{OrgID: orgID, UserID: userID}); err != nil {
			if isNotFound(err) {
				return notMember
			}
			return err
		}
		return q.AddGroupUser(ctx, sqlc.AddGroupUserParams{GroupID: groupID, UserID: userID, OrgID: orgID})
	})
}

// RemoveGroupMember takes a user out of a group; a user not in it (or an unknown group) is CodeNotFound.
func (s *Store) RemoveGroupMember(ctx context.Context, orgID, groupID, userID string) error {
	if !validID(userID) {
		return errNotFound
	}
	return s.withLockedGroup(ctx, orgID, groupID, func(q *sqlc.Queries) error {
		return notFoundIfZero(q.RemoveGroupUser(ctx, sqlc.RemoveGroupUserParams{OrgID: orgID, GroupID: groupID, UserID: userID}))
	})
}

// ReplaceGroupMembers makes userIDs the group's whole member set in one transaction and returns
// it. Every id must be an active member (CodeValidationFailed, nothing changed, otherwise); an
// unknown group is CodeNotFound. Duplicates collapse, and an empty list empties the group.
func (s *Store) ReplaceGroupMembers(ctx context.Context, orgID, groupID string, userIDs []string) ([]contract.Member, error) {
	if !validID(groupID) {
		return nil, errNotFound
	}
	notMember := contract.NewError(contract.CodeValidationFailed, "request validation failed", "userIds: every id must be an active member")
	ids := make([]string, 0, len(userIDs))
	seen := map[string]bool{}
	for _, id := range userIDs {
		if !validID(id) {
			return nil, notMember
		}
		if id = strings.ToLower(id); !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	err := s.withLockedGroup(ctx, orgID, groupID, func(q *sqlc.Queries) error {
		active, err := q.LockActiveMembershipsShared(ctx, sqlc.LockActiveMembershipsSharedParams{OrgID: orgID, UserIds: ids})
		if err != nil {
			return err
		}
		if len(active) != len(ids) {
			return notMember
		}
		if err := q.DeleteGroupUsersExcept(ctx, sqlc.DeleteGroupUsersExceptParams{OrgID: orgID, GroupID: groupID, UserIds: ids}); err != nil {
			return err
		}
		return q.AddGroupUsers(ctx, sqlc.AddGroupUsersParams{OrgID: orgID, GroupID: groupID, UserIds: ids})
	})
	if err != nil {
		return nil, err
	}
	return s.GroupMembers(ctx, orgID, groupID)
}
