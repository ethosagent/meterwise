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
