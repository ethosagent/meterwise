-- feature: users
-- +goose Up
CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES organizations (id),
    clerk_user_id text NOT NULL UNIQUE,
    email         text NOT NULL DEFAULT '',
    name          text NOT NULL DEFAULT '',
    avatar_url    text NOT NULL DEFAULT '',
    preferences   jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz
);

CREATE INDEX users_org_idx ON users (org_id);

-- +goose Down
DROP TABLE users;
