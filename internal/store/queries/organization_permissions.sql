-- name: ListOrgDefaults :many
SELECT COALESCE(workspace_id::text, '')::text AS workspace_id, resource_type, action
FROM organization_permissions WHERE org_id = sqlc.arg(org_id)::text::uuid
ORDER BY resource_type, action, workspace_id NULLS FIRST;

-- name: DeleteOrgDefaults :exec
DELETE FROM organization_permissions WHERE org_id = sqlc.arg(org_id)::text::uuid;

-- name: InsertOrgDefaults :exec
-- InsertOrgDefault for a whole list: parallel arrays, one grant per index (generate_subscripts; sqlc cannot analyse a multi-array unnest) ('' workspace = org-wide).
INSERT INTO organization_permissions (org_id, workspace_id, resource_type, action)
SELECT sqlc.arg(org_id)::text::uuid, NULLIF((sqlc.arg(workspace_ids)::text[])[i], '')::uuid,
       (sqlc.arg(resource_types)::text[])[i], (sqlc.arg(actions)::text[])[i]
FROM generate_subscripts(sqlc.arg(resource_types)::text[], 1) AS i
ON CONFLICT ON CONSTRAINT organization_permissions_identity DO NOTHING;

-- name: CountOrgWorkspacesAmong :one
-- How many of the given workspace ids belong to the org: one read validates a whole grant list.
SELECT count(*)::int AS n FROM workspaces
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = ANY(sqlc.arg(ids)::text[]::uuid[]);
