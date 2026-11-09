-- feature: invitations
-- +goose Up
CREATE TABLE invitations (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id              uuid NOT NULL REFERENCES organizations (id),
    email               text NOT NULL CHECK (email <> '' AND email = lower(btrim(email))),
    role                text NOT NULL CHECK (role IN ('admin', 'member')),
    clerk_invitation_id text,
    invited_by          uuid REFERENCES users (id),
    expires_at          timestamptz NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    accepted_at         timestamptz,
    revoked_at          timestamptz
);

-- One open (not accepted, not revoked) invitation per email.
CREATE UNIQUE INDEX invitations_open_email ON invitations (org_id, email)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

-- +goose Down
DROP TABLE invitations;
