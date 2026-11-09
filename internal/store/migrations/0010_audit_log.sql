-- feature: audit_log
-- +goose Up
-- The append-only record of who changed what (Plan 04). internal/audit is its only writer (H4).
-- workspace_id, actor_user_id and resource_id carry no foreign key on purpose: a row must outlive
-- the workspace or user it names, and a cascade or SET NULL would be an UPDATE or DELETE on it.
CREATE TABLE audit_log (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id        uuid NOT NULL REFERENCES organizations (id),
    workspace_id  uuid,
    actor_user_id uuid,
    action        text NOT NULL CHECK (action <> ''),
    resource_type text NOT NULL CHECK (resource_type <> ''),
    resource_id   text,
    ip            inet,
    metadata      jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_org_created_idx ON audit_log (org_id, created_at DESC, id DESC);

-- Append-only in the database, whatever role connects: a statement trigger refuses UPDATE, DELETE and
-- TRUNCATE (even one that matches no row) (a superuser could still drop the trigger, which is an operator act, not an app bug),
-- and the privileges are revoked from PUBLIC as a second wall for any non-owner role.
-- +goose StatementBegin
CREATE FUNCTION audit_log_refuse_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only: % is not allowed', TG_OP USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER audit_log_append_only BEFORE UPDATE OR DELETE OR TRUNCATE ON audit_log
    FOR EACH STATEMENT EXECUTE FUNCTION audit_log_refuse_change();

REVOKE UPDATE, DELETE, TRUNCATE ON audit_log FROM PUBLIC;

-- +goose Down
DROP TRIGGER audit_log_append_only ON audit_log;
DROP FUNCTION audit_log_refuse_change();
DROP TABLE audit_log;
