-- name: GetMembership :one
SELECT role, status FROM memberships
WHERE org_id = sqlc.arg(org_id)::text::uuid AND user_id = sqlc.arg(user_id)::text::uuid;

-- name: UpsertActiveMembership :exec
INSERT INTO memberships (org_id, user_id, role)
VALUES (sqlc.arg(org_id)::text::uuid, sqlc.arg(user_id)::text::uuid, sqlc.arg(role))
ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role, status = 'active', updated_at = now();

-- name: SetMembershipStatus :execrows
UPDATE memberships SET status = sqlc.arg(status), updated_at = now()
WHERE org_id = sqlc.arg(org_id)::text::uuid AND user_id = sqlc.arg(user_id)::text::uuid;

-- name: RemoveMembershipsForClerkUser :exec
UPDATE memberships SET status = 'removed', updated_at = now()
WHERE org_id = sqlc.arg(org_id)::text::uuid
  AND user_id IN (SELECT id FROM users WHERE org_id = sqlc.arg(org_id)::text::uuid AND clerk_user_id = sqlc.arg(clerk_user_id));

-- name: GetRemovedAdminByEmail :one
-- The user behind a removed admin membership, found by the mirrored email. Only the
-- operator-run bootstrap-admin command uses it; no request is ever authorised on it.
SELECT u.id::text AS id
FROM memberships m JOIN users u ON u.id = m.user_id AND u.org_id = m.org_id
WHERE m.org_id = sqlc.arg(org_id)::text::uuid AND u.email = sqlc.arg(email)
  AND m.role = 'admin' AND m.status = 'removed' AND u.deleted_at IS NULL
ORDER BY m.updated_at DESC
LIMIT 1;
