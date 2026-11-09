-- feature: memberships
-- +goose Up
CREATE TABLE memberships (
    org_id     uuid NOT NULL REFERENCES organizations (id),
    user_id    uuid NOT NULL REFERENCES users (id),
    role       text NOT NULL CHECK (role IN ('admin', 'member')),
    status     text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);

-- +goose Down
DROP TABLE memberships;
