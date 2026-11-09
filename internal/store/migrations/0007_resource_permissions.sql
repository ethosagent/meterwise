-- feature: resource_permissions
-- +goose Up
-- The catalog of grantable permissions: one row per (workspace, resource type, resource, action).
-- A group links to rows of this table (group_permissions, feature groups). workspace_id is NULL
-- for org-scoped resources. resource_id is reserved for per-resource grants and unused in v1.
-- The taxonomy (which resource types and actions exist) is code, not a table, so no CHECK here.
CREATE TABLE resource_permissions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES organizations (id),
    workspace_id  uuid REFERENCES workspaces (id) ON DELETE CASCADE,
    resource_type text NOT NULL CHECK (resource_type <> ''),
    resource_id   text,
    action        text NOT NULL CHECK (action <> ''),
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT resource_permissions_identity UNIQUE NULLS NOT DISTINCT (org_id, workspace_id, resource_type, resource_id, action)
);

-- +goose Down
DROP TABLE resource_permissions;
