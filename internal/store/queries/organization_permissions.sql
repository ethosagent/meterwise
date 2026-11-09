-- name: ListOrgDefaults :many
SELECT COALESCE(workspace_id::text, '')::text AS workspace_id, resource_type, action
FROM organization_permissions WHERE org_id = sqlc.arg(org_id)::text::uuid
ORDER BY resource_type, action, workspace_id NULLS FIRST;

-- name: DeleteOrgDefaults :exec
DELETE FROM organization_permissions WHERE org_id = sqlc.arg(org_id)::text::uuid;

-- name: InsertOrgDefault :exec
INSERT INTO organization_permissions (org_id, workspace_id, resource_type, action)
VALUES (sqlc.arg(org_id)::text::uuid, NULLIF(sqlc.arg(workspace_id)::text, '')::uuid, sqlc.arg(resource_type), sqlc.arg(action))
ON CONFLICT ON CONSTRAINT organization_permissions_identity DO NOTHING;
