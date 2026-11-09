-- name: GetMembership :one
SELECT role, status FROM memberships
WHERE org_id = sqlc.arg(org_id)::text::uuid AND user_id = sqlc.arg(user_id)::text::uuid;

-- name: UpsertActiveMembership :exec
INSERT INTO memberships (org_id, user_id, role)
VALUES (sqlc.arg(org_id)::text::uuid, sqlc.arg(user_id)::text::uuid, sqlc.arg(role))
ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role, status = 'active', updated_at = now(),
    created_at = CASE WHEN memberships.status = 'active' THEN memberships.created_at ELSE now() END;

-- name: SetMembershipStatus :execrows
UPDATE memberships SET status = sqlc.arg(status), updated_at = now()
WHERE org_id = sqlc.arg(org_id)::text::uuid AND user_id = sqlc.arg(user_id)::text::uuid;

-- name: RemoveMembershipsForClerkUser :exec
UPDATE memberships SET status = 'removed', updated_at = now()
WHERE org_id = sqlc.arg(org_id)::text::uuid
  AND user_id IN (SELECT id FROM users WHERE org_id = sqlc.arg(org_id)::text::uuid AND clerk_user_id = sqlc.arg(clerk_user_id));

-- name: ListMembers :many
SELECT u.id::text AS user_id, u.email, u.name, u.avatar_url, m.role, m.created_at AS joined_at,
       COALESCE((SELECT array_agg(gu.group_id::text ORDER BY gu.group_id) FROM group_users gu WHERE gu.user_id = u.id AND gu.org_id = m.org_id), '{}')::text[] AS group_ids
FROM memberships m JOIN users u ON u.id = m.user_id AND u.org_id = m.org_id
WHERE m.org_id = sqlc.arg(org_id)::text::uuid AND m.status = 'active' AND u.deleted_at IS NULL
ORDER BY u.email, u.id;

-- name: GetMember :one
SELECT u.id::text AS user_id, u.email, u.name, u.avatar_url, m.role, m.created_at AS joined_at,
       COALESCE((SELECT array_agg(gu.group_id::text ORDER BY gu.group_id) FROM group_users gu WHERE gu.user_id = u.id AND gu.org_id = m.org_id), '{}')::text[] AS group_ids
FROM memberships m JOIN users u ON u.id = m.user_id AND u.org_id = m.org_id
WHERE m.org_id = sqlc.arg(org_id)::text::uuid AND m.user_id = sqlc.arg(user_id)::text::uuid
  AND m.status = 'active' AND u.deleted_at IS NULL;

-- name: LockAdmins :exec
-- Serialises every change that could remove an admin, so the last-admin check cannot race.
SELECT pg_advisory_xact_lock(hashtext('admins:' || sqlc.arg(org_id)::text));

-- name: CountActiveAdmins :one
SELECT count(*)::int AS n FROM memberships
WHERE org_id = sqlc.arg(org_id)::text::uuid AND role = 'admin' AND status = 'active';

-- name: SetActiveMemberRole :execrows
UPDATE memberships SET role = sqlc.arg(role), updated_at = now()
WHERE org_id = sqlc.arg(org_id)::text::uuid AND user_id = sqlc.arg(user_id)::text::uuid AND status = 'active';

-- name: GetUserEmail :one
SELECT email FROM users WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid;

-- name: ActiveMemberEmailExists :one
-- Only a conflict check for invitations (already_member); never an authorisation.
SELECT EXISTS (
    SELECT 1 FROM memberships m JOIN users u ON u.id = m.user_id AND u.org_id = m.org_id
    WHERE m.org_id = sqlc.arg(org_id)::text::uuid AND m.status = 'active' AND u.deleted_at IS NULL AND u.email = sqlc.arg(email)
)::boolean AS exists;

-- name: LockActiveMembershipShared :one
-- Holds the membership row so a concurrent removal waits for the statement that relies on it.
SELECT role FROM memberships
WHERE org_id = sqlc.arg(org_id)::text::uuid AND user_id = sqlc.arg(user_id)::text::uuid AND status = 'active'
FOR SHARE;
