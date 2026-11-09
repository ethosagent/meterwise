package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// invRow is the column set every invitation query returns.
type invRow struct {
	ID, Email, Role, ClerkInvitationID, InvitedBy string
	ExpiresAt, CreatedAt, AcceptedAt, RevokedAt   pgtype.Timestamptz
}

func (r invRow) invitation() contract.Invitation {
	return contract.Invitation{
		ID: r.ID, Email: r.Email, Role: contract.Role(r.Role), ClerkInvitationID: r.ClerkInvitationID, InvitedBy: r.InvitedBy,
		ExpiresAt: ts(r.ExpiresAt), CreatedAt: ts(r.CreatedAt), AcceptedAt: tsp(r.AcceptedAt), RevokedAt: tsp(r.RevokedAt),
	}
}

// ListInvitations returns every invitation of the org, newest first.
func (s *Store) ListInvitations(ctx context.Context, orgID string) ([]contract.Invitation, error) {
	rows, err := s.q.ListInvitations(ctx, orgID)
	if err != nil {
		return nil, queryErr(err)
	}
	out := make([]contract.Invitation, 0, len(rows))
	for _, r := range rows {
		out = append(out, invRow(r).invitation())
	}
	return out, nil
}

// Invitation returns one invitation of the org.
func (s *Store) Invitation(ctx context.Context, orgID, id string) (contract.Invitation, error) {
	if !validID(id) {
		return contract.Invitation{}, errNotFound
	}
	r, err := s.q.GetInvitation(ctx, sqlc.GetInvitationParams{OrgID: orgID, ID: id})
	if err != nil {
		return contract.Invitation{}, queryErr(err)
	}
	return invRow(r).invitation(), nil
}

// OpenInvitation returns the unexpired open invitation for a normalised email;
// CodeNotFound when there is none.
func (s *Store) OpenInvitation(ctx context.Context, orgID, email string, now time.Time) (contract.Invitation, error) {
	r, err := s.q.GetOpenInvitationByEmail(ctx, sqlc.GetOpenInvitationByEmailParams{OrgID: orgID, Email: email, Now: pgts(now)})
	if err != nil {
		return contract.Invitation{}, queryErr(err)
	}
	return invRow(r).invitation(), nil
}

// NewInvitation is the input of CreateInvitation.
type NewInvitation struct {
	OrgID             string
	Email             string // normalised
	Role              contract.Role
	ClerkInvitationID string
	InvitedBy         string // user id; empty for the CLI bootstrap
	ExpiresAt         time.Time
	Now               time.Time
}

// CreateInvitation inserts an invitation. An email that belongs to an active member is
// CodeAlreadyMember (checked in the same transaction as the insert). An open invitation for the same email
// answers CodeDuplicateInvitation; an expired one is superseded first.
func (s *Store) CreateInvitation(ctx context.Context, in NewInvitation) (contract.Invitation, error) {
	var id string
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		member, err := q.ActiveMemberEmailExists(ctx, sqlc.ActiveMemberEmailExistsParams{OrgID: in.OrgID, Email: in.Email})
		if err != nil {
			return err
		}
		if member {
			return contract.NewError(contract.CodeAlreadyMember, "that email already belongs to an active member")
		}
		if err := q.SupersedeExpiredInvitations(ctx, sqlc.SupersedeExpiredInvitationsParams{Now: pgts(in.Now), OrgID: in.OrgID, Email: in.Email}); err != nil {
			return err
		}
		by := pgtype.Text{}
		if validID(in.InvitedBy) {
			by = pgtype.Text{String: in.InvitedBy, Valid: true}
		}
		id, err = q.InsertInvitation(ctx, sqlc.InsertInvitationParams{
			OrgID: in.OrgID, Email: in.Email, Role: string(in.Role),
			ClerkInvitationID: pgtype.Text{String: in.ClerkInvitationID, Valid: in.ClerkInvitationID != ""},
			InvitedBy:         by, ExpiresAt: pgts(in.ExpiresAt), Now: pgts(in.Now),
		})
		return err
	})
	if err != nil {
		if isUnique(err) {
			return contract.Invitation{}, contract.NewError(contract.CodeDuplicateInvitation, "an invitation for that email is already pending")
		}
		return contract.Invitation{}, queryErr(err)
	}
	return s.Invitation(ctx, in.OrgID, id)
}

// RevokeInvitation revokes an open invitation and returns its provider id ("" when none).
// CodeNotFound when the invitation is unknown or already accepted or revoked.
func (s *Store) RevokeInvitation(ctx context.Context, orgID, id string, now time.Time) (string, error) {
	if !validID(id) {
		return "", errNotFound
	}
	clerkID, err := s.q.RevokeInvitation(ctx, sqlc.RevokeInvitationParams{Now: pgts(now), OrgID: orgID, ID: id})
	return clerkID, queryErr(err)
}

// RefreshInvitation points an open invitation at a new provider invitation and expiry.
func (s *Store) RefreshInvitation(ctx context.Context, orgID, id, clerkID string, expiresAt time.Time) error {
	if !validID(id) {
		return errNotFound
	}
	n, err := s.q.RefreshInvitation(ctx, sqlc.RefreshInvitationParams{
		ClerkInvitationID: pgtype.Text{String: clerkID, Valid: clerkID != ""}, ExpiresAt: pgts(expiresAt), OrgID: orgID, ID: id,
	})
	if err != nil {
		return queryErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}
