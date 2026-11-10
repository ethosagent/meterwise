-- feature: audit_log
-- +goose Up
-- Plan 05 carry L1: the audit list filters by action or workspace and pages by (created_at, id) keyset.
-- Without these the filtered list scans the whole org's log and sorts it. Index-only changes: no
-- column, no row, and the append-only trigger is untouched.
CREATE INDEX audit_log_org_action_created_idx ON audit_log (org_id, action, created_at DESC, id DESC);
CREATE INDEX audit_log_org_workspace_created_idx ON audit_log (org_id, workspace_id, created_at DESC, id DESC);

-- +goose Down
DROP INDEX audit_log_org_workspace_created_idx;
DROP INDEX audit_log_org_action_created_idx;
