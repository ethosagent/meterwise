-- name: ListInvitations :many
SELECT i.id::text AS id, i.email, i.role, COALESCE(i.clerk_invitation_id, '') AS clerk_invitation_id,
       COALESCE(u.email, '') AS invited_by, i.expires_at, i.created_at, i.accepted_at, i.revoked_at
FROM invitations i LEFT JOIN users u ON u.id = i.invited_by
WHERE i.org_id = sqlc.arg(org_id)::text::uuid
ORDER BY i.created_at DESC, i.id;

-- name: GetInvitation :one
SELECT i.id::text AS id, i.email, i.role, COALESCE(i.clerk_invitation_id, '') AS clerk_invitation_id,
       COALESCE(u.email, '') AS invited_by, i.expires_at, i.created_at, i.accepted_at, i.revoked_at
FROM invitations i LEFT JOIN users u ON u.id = i.invited_by
WHERE i.org_id = sqlc.arg(org_id)::text::uuid AND i.id = sqlc.arg(id)::text::uuid;

-- name: GetOpenInvitationByEmail :one
SELECT i.id::text AS id, i.email, i.role, COALESCE(i.clerk_invitation_id, '') AS clerk_invitation_id,
       COALESCE(u.email, '') AS invited_by, i.expires_at, i.created_at, i.accepted_at, i.revoked_at
FROM invitations i LEFT JOIN users u ON u.id = i.invited_by
WHERE i.org_id = sqlc.arg(org_id)::text::uuid AND i.email = sqlc.arg(email)
  AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > sqlc.arg(now)::timestamptz;

-- name: SupersedeExpiredInvitations :exec
UPDATE invitations SET revoked_at = sqlc.arg(now)::timestamptz
WHERE org_id = sqlc.arg(org_id)::text::uuid AND email = sqlc.arg(email)
  AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at <= sqlc.arg(now)::timestamptz;

-- name: InsertInvitation :one
INSERT INTO invitations (org_id, email, role, clerk_invitation_id, invited_by, expires_at, created_at)
VALUES (sqlc.arg(org_id)::text::uuid, sqlc.arg(email), sqlc.arg(role), sqlc.narg(clerk_invitation_id), sqlc.narg(invited_by)::text::uuid,
        sqlc.arg(expires_at)::timestamptz, sqlc.arg(now)::timestamptz)
RETURNING id::text AS id;

-- name: AcceptInvitation :execrows
UPDATE invitations SET accepted_at = sqlc.arg(now)::timestamptz
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid
  AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > sqlc.arg(now)::timestamptz;

-- name: RevokeInvitation :one
UPDATE invitations SET revoked_at = sqlc.arg(now)::timestamptz
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid
  AND accepted_at IS NULL AND revoked_at IS NULL
RETURNING COALESCE(clerk_invitation_id, '') AS clerk_invitation_id;

-- name: RefreshInvitation :execrows
UPDATE invitations SET clerk_invitation_id = sqlc.narg(clerk_invitation_id), expires_at = sqlc.arg(expires_at)::timestamptz
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid
  AND accepted_at IS NULL AND revoked_at IS NULL;

-- name: RevokeOpenInvitationsForEmail :many
UPDATE invitations SET revoked_at = sqlc.arg(now)::timestamptz
WHERE org_id = sqlc.arg(org_id)::text::uuid AND email = sqlc.arg(email)
  AND accepted_at IS NULL AND revoked_at IS NULL
RETURNING COALESCE(clerk_invitation_id, '')::text AS clerk_invitation_id;
