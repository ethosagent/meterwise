-- feature: organizations
-- +goose Up
CREATE TABLE organizations (
    id         uuid PRIMARY KEY,
    name       text NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Exactly one organization while the product is single-tenant (decision D4): every
-- row indexes the same constant, so a second insert violates uniqueness.
CREATE UNIQUE INDEX organizations_single ON organizations ((true));

-- The one seeded row; its id is store.OrgID().
INSERT INTO organizations (id, name) VALUES ('00000000-0000-0000-0000-000000000001', 'Meterwise');

-- +goose Down
DROP TABLE organizations;
