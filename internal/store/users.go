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
func (s *Store) UpsertUser(ctx context.Context, orgID string, id contract.Identity) (contract.User, error) {
	// The common case (a returning user, nothing new in the claims) is one read, not a write.
	if u, err := s.q.GetUserByClerkID(ctx, sqlc.GetUserByClerkIDParams{OrgID: orgID, ClerkUserID: id.Subject}); err == nil {
		same := (id.Email == "" || strings.EqualFold(id.Email, u.Email)) && (id.Name == "" || id.Name == u.Name) && (id.AvatarURL == "" || id.AvatarURL == u.AvatarUrl)
		if same {
			return contract.User{ID: u.ID, ClerkUserID: u.ClerkUserID, Email: u.Email, Name: u.Name, AvatarURL: u.AvatarUrl, Deleted: u.Deleted}, nil
		}
	}
	r, err := s.q.UpsertUser(ctx, sqlc.UpsertUserParams{
		OrgID: orgID, ClerkUserID: id.Subject, Email: id.Email, Name: id.Name, AvatarUrl: id.AvatarURL,
	})
	if err != nil {
		return contract.User{}, queryErr(err)
	}
	return contract.User{ID: r.ID, ClerkUserID: r.ClerkUserID, Email: r.Email, Name: r.Name, AvatarURL: r.AvatarUrl, Deleted: r.Deleted}, nil
}

// SoftDeleteUser marks the user deleted and removes their memberships in one
// transaction. An unknown clerk user id is not an error (the webhook may arrive
// for a user that never signed in here).
func (s *Store) SoftDeleteUser(ctx context.Context, orgID, clerkUserID string) error {
	return queryErr(s.inTx(ctx, func(q *sqlc.Queries) error {
		if _, err := q.SoftDeleteUser(ctx, sqlc.SoftDeleteUserParams{OrgID: orgID, ClerkUserID: clerkUserID}); err != nil && !isNotFound(err) {
			return err
		}
		return q.RemoveMembershipsForClerkUser(ctx, sqlc.RemoveMembershipsForClerkUserParams{OrgID: orgID, ClerkUserID: clerkUserID})
	}))
}

// Preferences returns the stored UI preferences and the active workspace id ("" when unset).
func (s *Store) Preferences(ctx context.Context, orgID, userID string) (contract.Preferences, string, error) {
	if !validID(userID) {
		return contract.Preferences{}, "", errNotFound
	}
	raw, err := s.q.GetPreferences(ctx, sqlc.GetPreferencesParams{OrgID: orgID, UserID: userID})
	if err != nil {
		return contract.Preferences{}, "", queryErr(err)
	}
	return decodePreferences(raw)
}

// MergePreferences merges patch (top-level keys) into the user's preferences and returns the result.
func (s *Store) MergePreferences(ctx context.Context, orgID, userID string, patch map[string]any) (contract.Preferences, string, error) {
	if !validID(userID) {
		return contract.Preferences{}, "", errNotFound
	}
	b, err := json.Marshal(patch)
	if err != nil {
		return contract.Preferences{}, "", fail(ClassPool, "store: preferences patch is not JSON")
	}
	raw, err := s.q.MergePreferences(ctx, sqlc.MergePreferencesParams{Patch: b, OrgID: orgID, UserID: userID})
	if err != nil {
		return contract.Preferences{}, "", queryErr(err)
	}
	return decodePreferences(raw)
}

func decodePreferences(raw []byte) (contract.Preferences, string, error) {
	var stored struct {
		Theme             contract.Theme `json:"theme"`
		ReducedMotion     bool           `json:"reducedMotion"`
		ActiveWorkspaceID string         `json:"activeWorkspaceId"`
	}
	_ = json.Unmarshal(raw, &stored)
	if !stored.Theme.Valid() {
		stored.Theme = contract.ThemeSystem
	}
	return contract.Preferences{Theme: stored.Theme, ReducedMotion: stored.ReducedMotion}, stored.ActiveWorkspaceID, nil
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
