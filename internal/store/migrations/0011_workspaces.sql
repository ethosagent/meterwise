-- feature: workspaces
-- +goose Up
-- Plan 04: the workspace URL. last_call_at is touched by the ledger (Plan 05); allowed_client_cidrs
-- is the optional "only these source networks" guard (empty = off). The index makes "exactly one
-- current public id per workspace" a database fact: a reset retires the old row before inserting.
-- (Named for the workspaces feature, not workspace_url: the lint lets a migration change only the
-- tables of its own feature, and these are workspaces tables.)
ALTER TABLE workspaces ADD COLUMN last_call_at timestamptz;
ALTER TABLE workspaces ADD COLUMN allowed_client_cidrs cidr[] NOT NULL DEFAULT '{}';
CREATE UNIQUE INDEX workspace_public_ids_one_current ON workspace_public_ids (workspace_id) WHERE retired_at IS NULL;

-- +goose Down
DROP INDEX workspace_public_ids_one_current;
ALTER TABLE workspaces DROP COLUMN allowed_client_cidrs;
ALTER TABLE workspaces DROP COLUMN last_call_at;
