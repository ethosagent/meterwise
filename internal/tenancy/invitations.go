package tenancy

import (
	"context"
	"log/slog"

	"github.com/ethosagent/meterwise/internal/access"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/identity"
	"github.com/ethosagent/meterwise/internal/store"
)

// ListInvitations returns the org's invitations.
func (s *Service) ListInvitations(ctx context.Context, p contract.Principal) ([]contract.Invitation, error) {
	return s.st.ListInvitations(ctx, p.OrgID)
}

// provider invitations are optional only when the provider is not configured
// (dev); a provider that is configured and fails is an error.
func (s *Service) providerInvite(ctx context.Context, email string) (string, error) {
	id, err := s.idp.CreateInvitation(ctx, email)
	if contract.AsError(err).Code == contract.CodeNotAvailable {
		return "", nil
	}
	return id, err
}

func (s *Service) invite(ctx context.Context, byUserID, rawEmail string, role contract.Role) (contract.Invitation, error) {
	email, err := inviteEmail(rawEmail)
	if err != nil {
		return contract.Invitation{}, err
	}
	if !role.Valid() {
		return contract.Invitation{}, contract.Invalid("role", "must be admin or member")
	}
	if member, err := s.st.ActiveMemberWithEmail(ctx, s.org, email); err != nil {
		return contract.Invitation{}, err
	} else if member {
		return contract.Invitation{}, contract.NewError(contract.CodeAlreadyMember, "that email already belongs to an active member")
	}
	now := s.cfg.Now()
	if _, err := s.st.OpenInvitation(ctx, s.org, email, now); err == nil {
		return contract.Invitation{}, contract.NewError(contract.CodeDuplicateInvitation, "an invitation for that email is already pending")
	}
	clerkID, err := s.providerInvite(ctx, email)
	if err != nil {
		return contract.Invitation{}, err
	}
	inv, err := s.st.CreateInvitation(ctx, store.NewInvitation{
		OrgID: s.org, Email: email, Role: role, ClerkInvitationID: clerkID, InvitedBy: byUserID,
		ExpiresAt: now.Add(identity.InvitationLifetime), Now: now,
	})
	if err != nil {
		s.compensate(ctx, "invite: database write failed", clerkID) // do not leave an orphan that emails the person
		return contract.Invitation{}, err
	}
	return inv, nil
}

// CreateInvitation invites an email as the caller. Only an admin may invite an admin
// (access.MayAssignRole); the route's member:manage grant alone is not enough for that.
func (s *Service) CreateInvitation(ctx context.Context, p contract.Principal, email string, role contract.Role) (contract.Invitation, error) {
	if err := access.MayAssignRole(p.Role, role); err != nil {
		return contract.Invitation{}, err
	}
	return s.invite(ctx, p.UserID, email, role)
}

// BootstrapAdmin is the operator's admin invitation, for when no admin is active (first install,
// or the last admin was deleted at the provider). While any admin is active it refuses with a
// conflict. It is idempotent: an open admin invitation for the email is returned with created=false.
func (s *Service) BootstrapAdmin(ctx context.Context, email string) (inv contract.Invitation, created bool, err error) {
	e, err := inviteEmail(email)
	if err != nil {
		return contract.Invitation{}, false, err
	}
	// The operator command is the way in when nobody can invite: it works only while no admin is
	// active, and it only ever issues an invitation. Nobody is re-activated by an email match.
	n, err := s.st.ActiveAdminCount(ctx, s.org)
	if err != nil {
		return contract.Invitation{}, false, err
	}
	if n > 0 {
		return contract.Invitation{}, false, contract.NewError(contract.CodeConflict,
			"an admin is already active; invite admins from Settings > Organization")
	}
	all, err := s.st.ListInvitations(ctx, s.org)
	if err != nil {
		return contract.Invitation{}, false, err
	}
	now := s.cfg.Now()
	for _, i := range all {
		if i.Email != e || i.StatusAt(now) != contract.InvitationPending {
			continue
		}
		if !access.IsAdmin(i.Role) {
			return contract.Invitation{}, false, contract.NewError(contract.CodeConflict,
				"an open invitation for that email has role "+string(i.Role)+"; revoke it in Settings, then run bootstrap-admin again")
		}
		return i, false, nil
	}
	inv, err = s.invite(ctx, "", e, contract.RoleAdmin)
	return inv, err == nil, err
}

// ResendInvitation replaces the provider invitation of an open (not accepted, not
// revoked) invitation and extends its expiry.
func (s *Service) ResendInvitation(ctx context.Context, p contract.Principal, id string) (contract.Invitation, error) {
	inv, err := s.st.Invitation(ctx, p.OrgID, id)
	if err != nil {
		return contract.Invitation{}, err
	}
	if err := access.MayAssignRole(p.Role, inv.Role); err != nil {
		return contract.Invitation{}, err
	}
	now := s.cfg.Now()
	if st := inv.StatusAt(now); st == contract.InvitationAccepted || st == contract.InvitationRevoked {
		return contract.Invitation{}, contract.NewError(contract.CodeNotFound, "not found")
	}
	if err := s.idp.RevokeInvitation(ctx, inv.ClerkInvitationID); err != nil && contract.AsError(err).Code != contract.CodeNotAvailable {
		return contract.Invitation{}, err
	}
	clerkID, err := s.providerInvite(ctx, inv.Email)
	if err != nil {
		return contract.Invitation{}, err
	}
	if err := s.st.RefreshInvitation(ctx, p.OrgID, id, clerkID, now.Add(identity.InvitationLifetime)); err != nil {
		// The row still names the revoked provider invitation; do not leave the new one emailing the person.
		s.compensate(ctx, "resend: database refresh failed", clerkID)
		return contract.Invitation{}, err
	}
	return s.st.Invitation(ctx, p.OrgID, id)
}

// RevokeInvitation revokes an open invitation. The database row decides access
// (complete-signup reads it), so a provider hiccup cannot leave the invitation usable.
func (s *Service) RevokeInvitation(ctx context.Context, p contract.Principal, id string) error {
	inv, err := s.st.Invitation(ctx, p.OrgID, id)
	if err != nil {
		return err
	}
	if err := access.MayAssignRole(p.Role, inv.Role); err != nil {
		return err
	}
	clerkID, err := s.st.RevokeInvitation(ctx, p.OrgID, id, s.cfg.Now())
	if err != nil {
		return err
	}
	s.compensate(ctx, "revoke: provider revoke failed", clerkID)
	return nil
}

// compensate revokes a provider invitation as best-effort cleanup. The database row is the
// access truth, so a failure changes no access, but it leaves an orphan at the provider, so
// it is logged (with the provider id, never the email) instead of swallowed.
func (s *Service) compensate(ctx context.Context, what, providerID string) {
	if providerID == "" {
		return
	}
	if err := s.idp.RevokeInvitation(ctx, providerID); err != nil && contract.AsError(err).Code != contract.CodeNotAvailable {
		s.cfg.Log.LogAttrs(ctx, slog.LevelWarn, "provider invitation cleanup failed",
			slog.String("op", what), slog.String("provider_invitation_id", providerID), slog.String("code", string(contract.AsError(err).Code)))
	}
}
