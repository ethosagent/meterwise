-- feature: organization_permissions
-- +goose Up
-- Org defaults: what every active member may do, whatever their groups. workspace_id NULL means
-- every workspace (and is the only legal value for org-scoped resources).
CREATE TABLE organization_permissions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES organizations (id),
    workspace_id  uuid REFERENCES workspaces (id) ON DELETE CASCADE,
    resource_type text NOT NULL CHECK (resource_type <> ''),
    action        text NOT NULL CHECK (action <> ''),
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT organization_permissions_identity UNIQUE NULLS NOT DISTINCT (org_id, workspace_id, resource_type, action)
);

-- Seed for the one organization (as 0002 seeds the org row): every member keeps reading what
-- Plan 02 members could read. Admins may remove these; nothing re-adds them.
INSERT INTO organization_permissions (org_id, resource_type, action) VALUES
    ('00000000-0000-0000-0000-000000000001', 'workspace', 'read'),
    ('00000000-0000-0000-0000-000000000001', 'workspace_url', 'read'),
    ('00000000-0000-0000-0000-000000000001', 'session', 'read'),
    ('00000000-0000-0000-0000-000000000001', 'monitor_config', 'read'),
    ('00000000-0000-0000-0000-000000000001', 'savings', 'read');

-- +goose Down
DROP TABLE organization_permissions;
