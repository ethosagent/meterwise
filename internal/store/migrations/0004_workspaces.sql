-- feature: workspaces
-- +goose Up
CREATE TABLE workspaces (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES organizations (id),
    public_id  text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-Za-z]{22}$'),
    name       text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    is_default boolean NOT NULL DEFAULT false,
    settings   jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX workspaces_org_idx ON workspaces (org_id, created_at);
CREATE UNIQUE INDEX workspaces_one_default ON workspaces (org_id) WHERE is_default;

-- Every public id a workspace ever had (ruling R9: the full column set, Plan 04 adds no
-- second definition). A reset marks the old row retired and sets grace_until, so the
-- old URL can answer 410 instead of 404.
CREATE TABLE workspace_public_ids (
    public_id    text PRIMARY KEY CHECK (public_id ~ '^[0-9A-Za-z]{22}$'),
    org_id       uuid NOT NULL REFERENCES organizations (id),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    retired_at   timestamptz,
    grace_until  timestamptz
);

CREATE INDEX workspace_public_ids_workspace_idx ON workspace_public_ids (workspace_id);

-- +goose Down
DROP TABLE workspace_public_ids;
DROP TABLE workspaces;
