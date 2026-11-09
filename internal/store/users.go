package store

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// UpsertUser is the lazy mirror of an identity: it creates the user row on first
// sight and refreshes non-empty profile fields afterwards. A soft-deleted user
// stays deleted (User.Deleted is true).
//
// users.email is written only from a verified address: an unverified claim must not be able to
// take over a mirrored address (nothing authorizes on it, but operator tools and the invitation
// conflict check read it).
func (s *Store) UpsertUser(ctx context.Context, orgID string, id contract.Identity) (contract.User, error) {
	email := id.Email
	if !id.EmailVerified {
		email = ""
	}
	r, err := s.q.UpsertUser(ctx, sqlc.UpsertUserParams{
		OrgID: orgID, ClerkUserID: id.Subject, Email: email, Name: id.Name, AvatarUrl: id.AvatarURL,
	})
	if err != nil {
		return contract.User{}, queryErr(err)
	}
	return userRow{ID: r.ID, ClerkUserID: r.ClerkUserID, Email: r.Email, Name: r.Name, AvatarURL: r.AvatarUrl, Deleted: r.Deleted}.user(), nil
}

// userRow is the column set the user queries share.
type userRow struct {
	ID, ClerkUserID, Email, Name, AvatarURL string
	Deleted                                 bool
}

func (r userRow) user() contract.User {
	return contract.User{ID: r.ID, ClerkUserID: r.ClerkUserID, Email: r.Email, Name: r.Name, AvatarURL: r.AvatarURL, Deleted: r.Deleted}
}

// claimsMatch reports whether the identity's claims add nothing to the stored row: empty or
// unverified claims never differ, and a verified email is compared case-insensitively.
func claimsMatch(id contract.Identity, r userRow) bool {
	return (id.Email == "" || !id.EmailVerified || strings.EqualFold(id.Email, r.Email)) &&
		(id.Name == "" || id.Name == r.Name) && (id.AvatarURL == "" || id.AvatarURL == r.AvatarURL)
}

// ResolveUser is the per-request read: the mirrored user and the membership in one
// round trip. Only when the user row is missing or the token claims differ from it
// does it fall back to the lazy upsert (so the zero-webhook mirror still works) and
// a second read of the membership.
func (s *Store) ResolveUser(ctx context.Context, orgID string, id contract.Identity) (u contract.User, role contract.Role, active bool, err error) {
	if r, rerr := s.q.ResolveUser(ctx, sqlc.ResolveUserParams{OrgID: orgID, ClerkUserID: id.Subject}); rerr == nil {
		row := userRow{ID: r.ID, ClerkUserID: r.ClerkUserID, Email: r.Email, Name: r.Name, AvatarURL: r.AvatarUrl, Deleted: r.Deleted}
		if claimsMatch(id, row) {
			return row.user(), contract.Role(r.Role), r.Status == "active", nil
		}
	}
	u, err = s.UpsertUser(ctx, orgID, id)
	if err != nil {
		return contract.User{}, "", false, err
	}
	role, active, _, err = s.Membership(ctx, orgID, u.ID)
	return u, role, active, err
}

// SoftDeleteUser marks the user deleted and removes their memberships in one
// transaction. An unknown clerk user id is not an error (the webhook may arrive
// for a user that never signed in here).
func (s *Store) SoftDeleteUser(ctx context.Context, orgID, clerkUserID string) error {
	return queryErr(s.inTx(ctx, func(q *sqlc.Queries) error {
		// The webhook still removes even the last admin (recovery is bootstrap-admin); it takes the
		// admin lock so it cannot interleave with a guarded demotion or removal.
		if _, err := lockAdmins(ctx, q, orgID); err != nil {
			return err
		}
		if _, err := q.SoftDeleteUser(ctx, sqlc.SoftDeleteUserParams{OrgID: orgID, ClerkUserID: clerkUserID}); err != nil && !isNotFound(err) {
			return err
		}
		if err := q.RemoveMembershipsForClerkUser(ctx, sqlc.RemoveMembershipsForClerkUserParams{OrgID: orgID, ClerkUserID: clerkUserID}); err != nil {
			return err
		}
		return q.DeleteGroupUsersOfClerkUser(ctx, sqlc.DeleteGroupUsersOfClerkUserParams{OrgID: orgID, ClerkUserID: clerkUserID})
	}))
}

// Profile returns the stored UI preferences and the stored active workspace id
// ("" when unset; it may name a workspace that no longer exists).
func (s *Store) Profile(ctx context.Context, orgID, userID string) (contract.Profile, error) {
	if !validID(userID) {
		return contract.Profile{}, errNotFound
	}
	raw, err := s.q.GetPreferences(ctx, sqlc.GetPreferencesParams{OrgID: orgID, UserID: userID})
	if err != nil {
		return contract.Profile{}, queryErr(err)
	}
	return decodeProfile(raw), nil
}

// MergeProfile merges patch (top-level keys) into the user's stored profile and returns the result.
func (s *Store) MergeProfile(ctx context.Context, orgID, userID string, patch map[string]any) (contract.Profile, error) {
	if !validID(userID) {
		return contract.Profile{}, errNotFound
	}
	b, err := json.Marshal(patch)
	if err != nil {
		return contract.Profile{}, fail(ClassPool, "store: preferences patch is not JSON")
	}
	raw, err := s.q.MergePreferences(ctx, sqlc.MergePreferencesParams{Patch: b, OrgID: orgID, UserID: userID})
	if err != nil {
		return contract.Profile{}, queryErr(err)
	}
	return decodeProfile(raw), nil
}

func decodeProfile(raw []byte) contract.Profile {
	var stored struct {
		Theme             contract.Theme `json:"theme"`
		ReducedMotion     bool           `json:"reducedMotion"`
		ActiveWorkspaceID string         `json:"activeWorkspaceId"`
	}
	_ = json.Unmarshal(raw, &stored)
	if !stored.Theme.Valid() {
		stored.Theme = contract.ThemeSystem
	}
	return contract.Profile{Preferences: contract.Preferences{Theme: stored.Theme, ReducedMotion: stored.ReducedMotion}, ActiveWorkspaceID: stored.ActiveWorkspaceID}
}

// inTx runs fn in one transaction. Errors are raw (so callers can test for a
// unique violation); callers pass them through queryErr before they leave the package.
func (s *Store) inTx(ctx context.Context, fn func(*sqlc.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Preferences and MergePreferences are thin views over Profile and MergeProfile, kept
// only until internal/store/sqlc/queries_test.go (a path the edit guard denies the
// implementer) is moved to the struct API. New code uses Profile.

// Preferences returns the stored preferences and active workspace id of a user.
func (s *Store) Preferences(ctx context.Context, orgID, userID string) (contract.Preferences, string, error) {
	p, err := s.Profile(ctx, orgID, userID)
	return p.Preferences, p.ActiveWorkspaceID, err
}

// MergePreferences is MergeProfile returning the legacy triple.
func (s *Store) MergePreferences(ctx context.Context, orgID, userID string, patch map[string]any) (contract.Preferences, string, error) {
	p, err := s.MergeProfile(ctx, orgID, userID, patch)
	return p.Preferences, p.ActiveWorkspaceID, err
}
