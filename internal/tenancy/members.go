package tenancy

import (
	"context"

	"github.com/ethosagent/meterwise/internal/contract"
)

// ListMembers returns the org's active members.
func (s *Service) ListMembers(ctx context.Context, p contract.Principal) ([]contract.Member, error) {
	return s.st.ListMembers(ctx, p.OrgID)
}

// SetMemberRole changes an active member's role. The last active admin cannot be demoted
// (contract.CodeLastAdmin); the check runs inside the transaction that writes the role.
func (s *Service) SetMemberRole(ctx context.Context, p contract.Principal, userID string, role contract.Role) (contract.Member, error) {
	if !role.Valid() {
		return contract.Member{}, invalid("role", "must be admin or member")
	}
	return s.st.SetMemberRole(ctx, p.OrgID, userID, role)
}

// RemoveMember removes an active member: status removed, group memberships deleted and the open
// invitations for that user's email revoked, in one transaction (the last active admin cannot be
// removed). The provider invitations behind the revoked rows are withdrawn afterwards, best effort:
// the database row is the access truth. The user's next request is 403 not_invited.
func (s *Service) RemoveMember(ctx context.Context, p contract.Principal, userID string) error {
	providerIDs, err := s.st.RemoveMember(ctx, p.OrgID, userID, s.cfg.Now())
	if err != nil {
		return err
	}
	for _, id := range providerIDs {
		s.compensate(ctx, "remove member: provider invitation revoke failed", id)
	}
	return nil
}
