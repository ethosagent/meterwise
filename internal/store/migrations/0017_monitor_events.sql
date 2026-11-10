-- feature: monitor_events
-- +goose Up
-- Plan 06 (ruling R9): internal/ledger writes one monitor_events row per verdict, in the same transaction as
-- its calls row. The unique key is the idempotency backstop (a replayed call_id never doubles an event and a
-- monitor fires at most once per call); the two indexes serve the per-monitor stats (workspace, monitor, ts)
-- and the session detail (workspace, session, call index). Index-only: no column, no row.
CREATE UNIQUE INDEX monitor_events_call_monitor_uniq ON monitor_events (workspace_id, call_id, monitor);
CREATE INDEX monitor_events_workspace_monitor_ts_idx ON monitor_events (workspace_id, monitor, ts DESC);
CREATE INDEX monitor_events_workspace_session_idx ON monitor_events (workspace_id, session_id, call_index);

-- +goose Down
DROP INDEX monitor_events_workspace_session_idx;
DROP INDEX monitor_events_workspace_monitor_ts_idx;
DROP INDEX monitor_events_call_monitor_uniq;
