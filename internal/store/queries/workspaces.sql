-- name: ListWorkspaces :many
SELECT id::text AS id, org_id::text AS org_id, public_id, name, is_default, settings, created_at,
       last_call_at, allowed_client_cidrs::text[] AS allowed_client_cidrs
FROM workspaces WHERE org_id = sqlc.arg(org_id)::text::uuid
ORDER BY is_default DESC, created_at, id;

-- name: GetWorkspace :one
SELECT id::text AS id, org_id::text AS org_id, public_id, name, is_default, settings, created_at,
       last_call_at, allowed_client_cidrs::text[] AS allowed_client_cidrs
FROM workspaces WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid;

-- name: GetWorkspaceByID :one
-- The data-plane lookup: the row carries its org so the caller can check it.
SELECT id::text AS id, org_id::text AS org_id, public_id, name, is_default, settings, created_at,
       last_call_at, allowed_client_cidrs::text[] AS allowed_client_cidrs
FROM workspaces WHERE id = sqlc.arg(id)::text::uuid;

-- name: GetWorkspaceByPublicID :one
-- Current and retired ids both resolve; retired and grace_until tell the caller which state the id
-- is in (a retired id past its grace window is a reset URL, which is not "unknown").
SELECT w.id::text AS id, w.org_id::text AS org_id, p.public_id, w.name, w.is_default, w.settings, w.created_at,
       w.last_call_at, w.allowed_client_cidrs::text[] AS allowed_client_cidrs,
       (p.retired_at IS NOT NULL)::boolean AS retired, p.grace_until
FROM workspace_public_ids p JOIN workspaces w ON w.id = p.workspace_id
WHERE p.public_id = sqlc.arg(public_id);

-- name: LockWorkspaceCreation :exec
SELECT pg_advisory_xact_lock(hashtext('workspaces:' || sqlc.arg(org_id)::text));

-- name: CountWorkspaces :one
SELECT count(*)::int AS n FROM workspaces WHERE org_id = sqlc.arg(org_id)::text::uuid;

-- name: InsertWorkspace :one
INSERT INTO workspaces (org_id, public_id, name, is_default)
VALUES (sqlc.arg(org_id)::text::uuid, sqlc.arg(public_id), sqlc.arg(name), sqlc.arg(is_default))
RETURNING id::text AS id, org_id::text AS org_id, public_id, name, is_default, settings, created_at,
          last_call_at, allowed_client_cidrs::text[] AS allowed_client_cidrs;

-- name: InsertDefaultWorkspace :one
INSERT INTO workspaces (org_id, public_id, name, is_default)
SELECT sqlc.arg(org_id)::text::uuid, sqlc.arg(public_id), sqlc.arg(name), true
WHERE NOT EXISTS (SELECT 1 FROM workspaces WHERE org_id = sqlc.arg(org_id)::text::uuid AND is_default)
RETURNING id::text AS id;

-- name: InsertPublicID :exec
INSERT INTO workspace_public_ids (public_id, org_id, workspace_id)
VALUES (sqlc.arg(public_id), sqlc.arg(org_id)::text::uuid, sqlc.arg(workspace_id)::text::uuid);

-- name: UpdateWorkspace :one
-- A settings replacement keeps settings.monitors (Plan 06): the monitors have their own validated route, so the
-- replacement's own "monitors" key is dropped and the stored one is put back, atomically in this statement.
UPDATE workspaces SET name = COALESCE(sqlc.narg(name), name),
    settings = CASE WHEN sqlc.narg(settings)::jsonb IS NULL THEN settings
                    ELSE (sqlc.narg(settings)::jsonb - 'monitors')
                         || CASE WHEN settings->'monitors' IS NOT NULL THEN jsonb_build_object('monitors', settings->'monitors')
                                 ELSE '{}'::jsonb END END
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid
RETURNING id::text AS id, org_id::text AS org_id, public_id, name, is_default, settings, created_at,
          last_call_at, allowed_client_cidrs::text[] AS allowed_client_cidrs;

-- name: DeleteWorkspace :execrows
DELETE FROM workspaces
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid AND NOT is_default;

-- name: LockWorkspace :one
-- Serialises URL resets and CIDR changes of one workspace.
SELECT id::text AS id FROM workspaces
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid
FOR UPDATE;

-- name: RetireCurrentPublicID :one
-- Moves the workspace's current id to retired, valid until grace_until. Exactly one row is current.
UPDATE workspace_public_ids SET retired_at = sqlc.arg(now)::timestamptz, grace_until = sqlc.arg(grace_until)::timestamptz
WHERE org_id = sqlc.arg(org_id)::text::uuid AND workspace_id = sqlc.arg(workspace_id)::text::uuid AND retired_at IS NULL
RETURNING public_id;

-- name: SetWorkspacePublicID :exec
UPDATE workspaces SET public_id = sqlc.arg(public_id)
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid;

-- name: EndPreviousGrace :execrows
-- Ends the grace of every retired id of the workspace that is still inside it.
UPDATE workspace_public_ids SET grace_until = sqlc.arg(now)::timestamptz
WHERE org_id = sqlc.arg(org_id)::text::uuid AND workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND retired_at IS NOT NULL AND grace_until > sqlc.arg(now)::timestamptz;

-- name: SetAllowedClientCIDRs :one
UPDATE workspaces SET allowed_client_cidrs = sqlc.arg(cidrs)::text[]::cidr[]
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid
RETURNING allowed_client_cidrs::text[] AS allowed_client_cidrs;

-- name: LockWorkspaceSettings :one
-- The settings of one org workspace, row-locked for a read-modify-write of one key (the monitors).
SELECT settings FROM workspaces
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid
FOR UPDATE;

-- name: SetWorkspaceMonitors :one
-- Replace only settings.monitors; every other key of the settings is kept.
UPDATE workspaces SET settings = jsonb_set(settings, '{monitors}', sqlc.arg(monitors)::jsonb, true)
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid
RETURNING settings;
