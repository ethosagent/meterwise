package identity

import (
	"context"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Disabled is the IdentityProvider of a process with no Clerk configuration
// (dev only; boot refuses it elsewhere): every token is rejected and invitations
// are not available, so the database row is the only record of an invitation.
type Disabled struct{}

var _ contract.IdentityProvider = Disabled{}

// Verify rejects every token.
func (Disabled) Verify(context.Context, string) (contract.Identity, error) {
	return contract.Identity{}, unauthenticated()
}

// CreateInvitation is not available without Clerk.
func (Disabled) CreateInvitation(context.Context, string) (string, error) {
	return "", contract.Unavailable("Clerk", "no invitation email is sent")
}

// RevokeInvitation is a no-op without Clerk.
func (Disabled) RevokeInvitation(context.Context, string) error { return nil }
