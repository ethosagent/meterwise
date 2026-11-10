package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// UpdateWorkspaceMonitors changes settings.monitors of an org workspace and nothing else in its settings.
// The row is locked for the read-modify-write, so two PATCHes of different monitors both land. apply gets the
// current config (defaults filled where the stored one is missing or partial) and returns the new one; an
// error from apply, such as a validation error, rolls back and is returned as is. An unknown workspace or one
// of another org is not_found.
func (s *Store) UpdateWorkspaceMonitors(ctx context.Context, orgID, id string,
	apply func(contract.MonitorConfig) (contract.MonitorConfig, error)) (contract.MonitorConfig, error) {
	if !validID(id) {
		return contract.MonitorConfig{}, errNotFound
	}
	var out contract.MonitorConfig
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		raw, err := q.LockWorkspaceSettings(ctx, sqlc.LockWorkspaceSettingsParams{OrgID: orgID, ID: id})
		if err != nil {
			return err
		}
		next, err := apply(contract.MonitorConfigFrom(settingsOf(raw)["monitors"]))
		if err != nil {
			return err
		}
		out = next
		_, err = q.SetWorkspaceMonitors(ctx, sqlc.SetWorkspaceMonitorsParams{OrgID: orgID, ID: id, Monitors: []byte(next.Attr())})
		return err
	})
	return out, queryErr(err)
}
