-- feature: groups
-- +goose Up
CREATE TABLE groups (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES organizations (id),
    name       text NOT NULL CHECK (length(name) BETWEEN 1 AND 80 AND name = btrim(name)),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX groups_name_unique ON groups (org_id, lower(name));

-- Which workspaces a group is attached to.
CREATE TABLE group_workspaces (
    group_id     uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    org_id       uuid NOT NULL REFERENCES organizations (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, workspace_id)
);

-- Who is in a group. Only active members are ever in it; removing a member deletes the rows.
CREATE TABLE group_users (
    group_id   uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    org_id     uuid NOT NULL REFERENCES organizations (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX group_users_user_idx ON group_users (org_id, user_id);

-- What a group may do: links to the permission catalog.
CREATE TABLE group_permissions (
    group_id      uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    permission_id uuid NOT NULL REFERENCES resource_permissions (id) ON DELETE CASCADE,
    org_id        uuid NOT NULL REFERENCES organizations (id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, permission_id)
);

-- +goose Down
DROP TABLE group_permissions;
DROP TABLE group_users;
DROP TABLE group_workspaces;
DROP TABLE groups;
