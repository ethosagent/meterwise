-- name: ListGrantsForUser :many
-- The one read of authorization: every group grant of the user and every org default, in
-- one round trip. workspace_id and group_id are empty strings for NULL and "org default".
-- A group grant counts only while its group is still attached to the grant's workspace.
SELECT 'group'::text AS source, g.id::text AS group_id, COALESCE(rp.workspace_id::text, '')::text AS workspace_id,
       rp.resource_type, rp.action
FROM group_users gu
JOIN groups g ON g.id = gu.group_id AND g.org_id = gu.org_id
JOIN group_permissions gp ON gp.group_id = g.id AND gp.org_id = g.org_id
JOIN resource_permissions rp ON rp.id = gp.permission_id AND rp.org_id = g.org_id
WHERE gu.org_id = sqlc.arg(org_id)::text::uuid AND gu.user_id = sqlc.arg(user_id)::text::uuid
  AND (rp.workspace_id IS NULL OR EXISTS (
        SELECT 1 FROM group_workspaces gw WHERE gw.group_id = g.id AND gw.workspace_id = rp.workspace_id))
UNION ALL
SELECT 'org'::text, ''::text, COALESCE(op.workspace_id::text, '')::text, op.resource_type, op.action
FROM organization_permissions op
WHERE op.org_id = sqlc.arg(org_id)::text::uuid;

-- name: ListGroups :many
SELECT g.id::text AS id, g.name, g.created_at,
       (SELECT count(*) FROM group_users gu WHERE gu.group_id = g.id)::int AS member_count,
       COALESCE((SELECT array_agg(gw.workspace_id::text ORDER BY gw.workspace_id) FROM group_workspaces gw WHERE gw.group_id = g.id), '{}')::text[] AS workspace_ids
FROM groups g WHERE g.org_id = sqlc.arg(org_id)::text::uuid
ORDER BY lower(g.name), g.id;

-- name: GetGroup :one
SELECT g.id::text AS id, g.name, g.created_at,
       (SELECT count(*) FROM group_users gu WHERE gu.group_id = g.id)::int AS member_count,
       COALESCE((SELECT array_agg(gw.workspace_id::text ORDER BY gw.workspace_id) FROM group_workspaces gw WHERE gw.group_id = g.id), '{}')::text[] AS workspace_ids
FROM groups g WHERE g.org_id = sqlc.arg(org_id)::text::uuid AND g.id = sqlc.arg(id)::text::uuid;

-- name: InsertGroup :one
INSERT INTO groups (org_id, name) VALUES (sqlc.arg(org_id)::text::uuid, sqlc.arg(name))
RETURNING id::text AS id;

-- name: RenameGroup :execrows
UPDATE groups SET name = sqlc.arg(name)
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid;

-- name: DeleteGroup :execrows
DELETE FROM groups WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid;

-- name: LockGroup :one
-- Serialises attach, detach and replace-grants on one group, so a grant can never be written
-- for a workspace that a concurrent detach has just removed.
SELECT id::text AS id FROM groups
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid
FOR UPDATE;

-- name: AttachGroupWorkspace :execrows
-- Rows affected is 1 only when the link is new: an attached group is not seeded again.
INSERT INTO group_workspaces (group_id, workspace_id, org_id)
VALUES (sqlc.arg(group_id)::text::uuid, sqlc.arg(workspace_id)::text::uuid, sqlc.arg(org_id)::text::uuid)
ON CONFLICT DO NOTHING;

-- name: DetachGroupWorkspace :execrows
DELETE FROM group_workspaces
WHERE org_id = sqlc.arg(org_id)::text::uuid AND group_id = sqlc.arg(group_id)::text::uuid AND workspace_id = sqlc.arg(workspace_id)::text::uuid;

-- name: DeleteGroupGrantsOnWorkspace :exec
DELETE FROM group_permissions gp
USING resource_permissions rp
WHERE rp.id = gp.permission_id AND gp.org_id = sqlc.arg(org_id)::text::uuid
  AND gp.group_id = sqlc.arg(group_id)::text::uuid AND rp.workspace_id = sqlc.arg(workspace_id)::text::uuid;

-- name: ListGroupGrants :many
-- One read for the group and its grants: a missing group yields no row at all (not_found), a group
-- without grants yields one row whose resource_type is empty.
SELECT COALESCE(rp.workspace_id::text, '')::text AS workspace_id, COALESCE(rp.resource_type, '')::text AS resource_type,
       COALESCE(rp.action, '')::text AS action
FROM groups g
LEFT JOIN group_permissions gp ON gp.group_id = g.id AND gp.org_id = g.org_id
LEFT JOIN resource_permissions rp ON rp.id = gp.permission_id
WHERE g.org_id = sqlc.arg(org_id)::text::uuid AND g.id = sqlc.arg(group_id)::text::uuid
ORDER BY rp.resource_type NULLS FIRST, rp.action, rp.workspace_id NULLS FIRST;

-- name: DeleteGroupGrants :exec
DELETE FROM group_permissions
WHERE org_id = sqlc.arg(org_id)::text::uuid AND group_id = sqlc.arg(group_id)::text::uuid;

-- name: GrantToGroup :exec
-- Get-or-create the catalog row, then link it. workspace_id is '' for an org-scoped grant.
WITH perm AS (
    INSERT INTO resource_permissions (org_id, workspace_id, resource_type, action)
    VALUES (sqlc.arg(org_id)::text::uuid, NULLIF(sqlc.arg(workspace_id)::text, '')::uuid, sqlc.arg(resource_type), sqlc.arg(action))
    ON CONFLICT ON CONSTRAINT resource_permissions_identity DO UPDATE SET action = EXCLUDED.action
    RETURNING id
)
INSERT INTO group_permissions (group_id, permission_id, org_id)
SELECT sqlc.arg(group_id)::text::uuid, perm.id, sqlc.arg(org_id)::text::uuid FROM perm
ON CONFLICT DO NOTHING;

-- name: GrantManyToGroup :exec
-- GrantToGroup for a whole list in one statement: the parallel arrays are one grant per index
-- (workspace_ids holds the empty string for an org-wide grant; generate_subscripts because sqlc cannot analyse a multi-array unnest). DISTINCT keeps ON CONFLICT from seeing a row twice.
WITH input AS (
    SELECT DISTINCT NULLIF((sqlc.arg(workspace_ids)::text[])[i], '')::uuid AS workspace_id,
           (sqlc.arg(resource_types)::text[])[i] AS resource_type, (sqlc.arg(actions)::text[])[i] AS action
    FROM generate_subscripts(sqlc.arg(resource_types)::text[], 1) AS i
), perm AS (
    INSERT INTO resource_permissions (org_id, workspace_id, resource_type, action)
    SELECT sqlc.arg(org_id)::text::uuid, workspace_id, resource_type, action FROM input
    ON CONFLICT ON CONSTRAINT resource_permissions_identity DO UPDATE SET action = EXCLUDED.action
    RETURNING id
)
INSERT INTO group_permissions (group_id, permission_id, org_id)
SELECT sqlc.arg(group_id)::text::uuid, perm.id, sqlc.arg(org_id)::text::uuid FROM perm
ON CONFLICT DO NOTHING;

-- name: ListGroupMembers :many
-- One read for the group and its active members: a missing group yields no row (not_found); a
-- group with no active member yields rows whose user_id is empty, which the caller skips.
SELECT COALESCE(u.id::text, '')::text AS user_id, COALESCE(u.email, '')::text AS email, COALESCE(u.name, '')::text AS name,
       COALESCE(u.avatar_url, '')::text AS avatar_url, COALESCE(m.role, '')::text AS role, m.created_at AS joined_at,
       COALESCE((SELECT array_agg(x.group_id::text ORDER BY x.group_id) FROM group_users x WHERE x.user_id = u.id AND x.org_id = g.org_id), '{}')::text[] AS group_ids
FROM groups g
LEFT JOIN group_users gu ON gu.group_id = g.id AND gu.org_id = g.org_id
LEFT JOIN memberships m ON m.user_id = gu.user_id AND m.org_id = gu.org_id AND m.status = 'active'
LEFT JOIN users u ON u.id = m.user_id AND u.org_id = m.org_id AND u.deleted_at IS NULL
WHERE g.org_id = sqlc.arg(org_id)::text::uuid AND g.id = sqlc.arg(group_id)::text::uuid
ORDER BY u.email, u.id;

-- name: AddGroupUser :exec
INSERT INTO group_users (group_id, user_id, org_id)
VALUES (sqlc.arg(group_id)::text::uuid, sqlc.arg(user_id)::text::uuid, sqlc.arg(org_id)::text::uuid)
ON CONFLICT DO NOTHING;

-- name: RemoveGroupUser :execrows
DELETE FROM group_users
WHERE org_id = sqlc.arg(org_id)::text::uuid AND group_id = sqlc.arg(group_id)::text::uuid AND user_id = sqlc.arg(user_id)::text::uuid;

-- name: DeleteGroupUsersOfUser :exec
DELETE FROM group_users WHERE org_id = sqlc.arg(org_id)::text::uuid AND user_id = sqlc.arg(user_id)::text::uuid;

-- name: DeleteGroupUsersOfClerkUser :exec
DELETE FROM group_users
WHERE org_id = sqlc.arg(org_id)::text::uuid
  AND user_id IN (SELECT id FROM users WHERE org_id = sqlc.arg(org_id)::text::uuid AND clerk_user_id = sqlc.arg(clerk_user_id));
