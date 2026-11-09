-- name: ListWorkspaces :many
SELECT id::text AS id, org_id::text AS org_id, public_id, name, is_default, settings, created_at
FROM workspaces WHERE org_id = sqlc.arg(org_id)::text::uuid
ORDER BY is_default DESC, created_at, id;

-- name: GetWorkspace :one
SELECT id::text AS id, org_id::text AS org_id, public_id, name, is_default, settings, created_at
FROM workspaces WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid;

-- name: GetWorkspaceByID :one
-- The data-plane lookup: the row carries its org so the caller can check it.
SELECT id::text AS id, org_id::text AS org_id, public_id, name, is_default, settings, created_at
FROM workspaces WHERE id = sqlc.arg(id)::text::uuid;

-- name: GetWorkspaceByPublicID :one
-- Current and retired ids both resolve; retired is true for an id a reset replaced.
-- A retired id past its grace window resolves to nothing.
SELECT w.id::text AS id, w.org_id::text AS org_id, p.public_id, w.name, w.is_default, w.settings, w.created_at,
       (p.retired_at IS NOT NULL)::boolean AS retired
FROM workspace_public_ids p JOIN workspaces w ON w.id = p.workspace_id
WHERE p.public_id = sqlc.arg(public_id)
  AND (p.retired_at IS NULL OR p.grace_until IS NULL OR p.grace_until > sqlc.arg(now)::timestamptz);

-- name: LockWorkspaceCreation :exec
SELECT pg_advisory_xact_lock(hashtext('workspaces:' || sqlc.arg(org_id)::text));

-- name: CountWorkspaces :one
SELECT count(*)::int AS n FROM workspaces WHERE org_id = sqlc.arg(org_id)::text::uuid;

-- name: InsertWorkspace :one
INSERT INTO workspaces (org_id, public_id, name, is_default)
VALUES (sqlc.arg(org_id)::text::uuid, sqlc.arg(public_id), sqlc.arg(name), sqlc.arg(is_default))
RETURNING id::text AS id, org_id::text AS org_id, public_id, name, is_default, settings, created_at;

-- name: InsertDefaultWorkspace :one
INSERT INTO workspaces (org_id, public_id, name, is_default)
SELECT sqlc.arg(org_id)::text::uuid, sqlc.arg(public_id), sqlc.arg(name), true
WHERE NOT EXISTS (SELECT 1 FROM workspaces WHERE org_id = sqlc.arg(org_id)::text::uuid AND is_default)
RETURNING id::text AS id;

-- name: InsertPublicID :exec
INSERT INTO workspace_public_ids (public_id, org_id, workspace_id)
VALUES (sqlc.arg(public_id), sqlc.arg(org_id)::text::uuid, sqlc.arg(workspace_id)::text::uuid);

-- name: UpdateWorkspace :one
UPDATE workspaces SET name = COALESCE(sqlc.narg(name), name), settings = COALESCE(sqlc.narg(settings)::jsonb, settings)
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid
RETURNING id::text AS id, org_id::text AS org_id, public_id, name, is_default, settings, created_at;

-- name: DeleteWorkspace :execrows
DELETE FROM workspaces
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid AND NOT is_default;
