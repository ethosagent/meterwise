package store

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// UpdateWorkspaceSetting is the generic read-modify-write of one saver key (trim, router or memory) of an org
// workspace's settings, and nothing else in them. The row is locked, so two PATCHes of different keys both land.
// apply gets what the key held (nil when missing) and returns the new canonical JSON; an error from apply, such as a
// validation error, rolls back and is returned as is. Any other key is a validation error on "key"; an unknown
// workspace or one of another org is not_found.
func (s *Store) UpdateWorkspaceSetting(ctx context.Context, orgID, id, key string, apply func(current any) (string, error)) error {
	if !slices.Contains(contract.SaverSettingKeys(), key) {
		return contract.Invalid("key", "must be one of trim, router, memory")
	}
	if !validID(id) {
		return errNotFound
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		raw, err := q.LockWorkspaceSettings(ctx, sqlc.LockWorkspaceSettingsParams{OrgID: orgID, ID: id})
		if err != nil {
			return err
		}
		next, err := apply(settingsOf(raw)[key])
		if err != nil {
			return err
		}
		_, err = q.SetWorkspaceSetting(ctx, sqlc.SetWorkspaceSettingParams{OrgID: orgID, ID: id, Key: key, Value: []byte(next)})
		return err
	})
	return queryErr(err)
}
