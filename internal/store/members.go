package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

func toMember(userID, email, name, avatar, role string, joined pgtype.Timestamptz, groups []string) contract.Member {
	if groups == nil {
		groups = []string{}
	}
	return contract.Member{
		UserID: userID, Email: email, Name: name, AvatarURL: avatar, Role: contract.Role(role),
		GroupIDs: groups, JoinedAt: ts(joined).Format(time.RFC3339),
	}
}

// ListMembers returns the org's active members, by email.
func (s *Store) ListMembers(ctx context.Context, orgID string) ([]contract.Member, error) {
	rows, err := s.q.ListMembers(ctx, orgID)
	if err != nil {
		return nil, queryErr(err)
	}
	out := make([]contract.Member, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMember(r.UserID, r.Email, r.Name, r.AvatarUrl, r.Role, r.JoinedAt, r.GroupIds))
	}
	return out, nil
}

// Member returns one active member; anyone else (removed, deleted, never a member, another org)
// is CodeNotFound.
func (s *Store) Member(ctx context.Context, orgID, userID string) (contract.Member, error) {
	return member(ctx, s.q, orgID, userID)
}

func member(ctx context.Context, q *sqlc.Queries, orgID, userID string) (contract.Member, error) {
	if !validID(userID) {
		return contract.Member{}, errNotFound
	}
	r, err := q.GetMember(ctx, sqlc.GetMemberParams{OrgID: orgID, UserID: userID})
	if err != nil {
		return contract.Member{}, queryErr(err)
	}
	return toMember(r.UserID, r.Email, r.Name, r.AvatarUrl, r.Role, r.JoinedAt, r.GroupIds), nil
}

// ActiveMemberWithEmail reports whether an active member's mirrored email is email (normalised).
// It only decides the already_member conflict on an invitation; it never authorises anything.
func (s *Store) ActiveMemberWithEmail(ctx context.Context, orgID, email string) (bool, error) {
	ok, err := s.q.ActiveMemberEmailExists(ctx, sqlc.ActiveMemberEmailExistsParams{OrgID: orgID, Email: email})
	return ok, queryErr(err)
}

// ActiveAdminCount is the number of active admin memberships. It takes no lock: callers that
// need the count to stay true (last-admin guards) go through lockAdmins instead.
func (s *Store) ActiveAdminCount(ctx context.Context, orgID string) (int, error) {
	n, err := s.q.CountActiveAdmins(ctx, orgID)
	return int(n), queryErr(err)
}

// lockAdmins serialises admin-removing changes of the org and returns the active admin count.
// It must run inside a transaction: the advisory lock lives until commit, so two concurrent
// demotions cannot both see two admins.
func lockAdmins(ctx context.Context, q *sqlc.Queries, orgID string) (int, error) {
	if err := q.LockAdmins(ctx, orgID); err != nil {
		return 0, err
	}
	n, err := q.CountActiveAdmins(ctx, orgID)
	return int(n), err
}

func lastAdmin() error {
	return contract.NewError(contract.CodeLastAdmin, "the organization must keep at least one active admin")
}

// SetMemberRole changes an active member's role. Demoting the last active admin is
// CodeLastAdmin; the check runs under the admin lock, inside the same transaction as the write.
func (s *Store) SetMemberRole(ctx context.Context, orgID, userID string, role contract.Role) (contract.Member, error) {
	if !validID(userID) {
		return contract.Member{}, errNotFound
	}
	var out contract.Member
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		admins, err := lockAdmins(ctx, q, orgID)
		if err != nil {
			return err
		}
		cur, err := q.GetMembership(ctx, sqlc.GetMembershipParams{OrgID: orgID, UserID: userID})
		if err != nil {
			return err
		}
		if cur.Status != "active" {
			return errNotFound
		}
		if contract.Role(cur.Role) != role && cur.Role == string(contract.RoleAdmin) && admins <= 1 {
			return lastAdmin()
		}
		if _, err := q.SetActiveMemberRole(ctx, sqlc.SetActiveMemberRoleParams{Role: string(role), OrgID: orgID, UserID: userID}); err != nil {
			return err
		}
		out, err = member(ctx, q, orgID, userID)
		return err
	})
	return out, queryErr(err)
}

// RemoveMember removes an active member in one transaction: the membership becomes removed, the
// user leaves every group, and the open invitations for that user's email are revoked. It returns
// the provider ids of the revoked invitations so the caller can withdraw them. Removing the last
// active admin is CodeLastAdmin (checked under the admin lock).
func (s *Store) RemoveMember(ctx context.Context, orgID, userID string, now time.Time) (providerIDs []string, err error) {
	if !validID(userID) {
		return nil, errNotFound
	}
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		admins, err := lockAdmins(ctx, q, orgID)
		if err != nil {
			return err
		}
		cur, err := q.GetMembership(ctx, sqlc.GetMembershipParams{OrgID: orgID, UserID: userID})
		if err != nil {
			return err
		}
		if cur.Status != "active" {
			return errNotFound
		}
		if cur.Role == string(contract.RoleAdmin) && admins <= 1 {
			return lastAdmin()
		}
		if _, err := q.SetMembershipStatus(ctx, sqlc.SetMembershipStatusParams{Status: "removed", OrgID: orgID, UserID: userID}); err != nil {
			return err
		}
		if err := q.DeleteGroupUsersOfUser(ctx, sqlc.DeleteGroupUsersOfUserParams{OrgID: orgID, UserID: userID}); err != nil {
			return err
		}
		email, err := q.GetUserEmail(ctx, sqlc.GetUserEmailParams{OrgID: orgID, ID: userID})
		if err != nil {
			return err
		}
		if email == "" {
			return nil
		}
		providerIDs, err = q.RevokeOpenInvitationsForEmail(ctx, sqlc.RevokeOpenInvitationsForEmailParams{Now: pgts(now), OrgID: orgID, Email: email})
		return err
	})
	return providerIDs, queryErr(err)
}
