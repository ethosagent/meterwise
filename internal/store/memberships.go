package store

import (
	"context"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// Membership returns the user's membership role and whether it is active.
// found is false when the user has never been a member.
func (s *Store) Membership(ctx context.Context, orgID, userID string) (role contract.Role, active, found bool, err error) {
	if !validID(userID) {
		return "", false, false, nil
	}
	r, err := s.q.GetMembership(ctx, sqlc.GetMembershipParams{OrgID: orgID, UserID: userID})
	if err != nil {
		if isNotFound(err) {
			return "", false, false, nil
		}
		return "", false, false, queryErr(err)
	}
	return contract.Role(r.Role), r.Status == "active", true, nil
}

// SetMembershipStatus sets a membership to "active" or "removed" (Plan 03 owns the endpoint).
func (s *Store) SetMembershipStatus(ctx context.Context, orgID, userID, status string) error {
	if !validID(userID) {
		return errNotFound
	}
	n, err := s.q.SetMembershipStatus(ctx, sqlc.SetMembershipStatusParams{Status: status, OrgID: orgID, UserID: userID})
	if err != nil {
		return queryErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

// AcceptOpenInvitation consumes the open invitation for email and makes userID an
// active member with the invited role, atomically. ok is false when no open
// invitation exists. email must already be normalised.
func (s *Store) AcceptOpenInvitation(ctx context.Context, orgID, userID, email string, now time.Time) (role contract.Role, ok bool, err error) {
	if !validID(userID) {
		return "", false, errNotFound
	}
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		inv, qerr := q.GetOpenInvitationByEmail(ctx, sqlc.GetOpenInvitationByEmailParams{OrgID: orgID, Email: email, Now: pgts(now)})
		if qerr != nil {
			if isNotFound(qerr) {
				return nil
			}
			return qerr
		}
		n, qerr := q.AcceptInvitation(ctx, sqlc.AcceptInvitationParams{Now: pgts(now), OrgID: orgID, ID: inv.ID})
		if qerr != nil || n == 0 {
			return qerr
		}
		if qerr = q.UpsertActiveMembership(ctx, sqlc.UpsertActiveMembershipParams{OrgID: orgID, UserID: userID, Role: inv.Role}); qerr != nil {
			return qerr
		}
		role, ok = contract.Role(inv.Role), true
		return nil
	})
	return role, ok, queryErr(err)
}

// UpsertActiveMembership makes userID an active member with role (used by tests and Plan 03).
func (s *Store) UpsertActiveMembership(ctx context.Context, orgID, userID string, role contract.Role) error {
	if !validID(userID) {
		return errNotFound
	}
	return queryErr(s.q.UpsertActiveMembership(ctx, sqlc.UpsertActiveMembershipParams{OrgID: orgID, UserID: userID, Role: string(role)}))
}
