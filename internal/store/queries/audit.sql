-- name: InsertAudit :exec
-- The only INSERT into audit_log (H4). Empty strings stand for NULL (workspace, actor, resource id, ip).
INSERT INTO audit_log (org_id, workspace_id, actor_user_id, action, resource_type, resource_id, ip, metadata, created_at)
VALUES (sqlc.arg(org_id)::text::uuid, NULLIF(sqlc.arg(workspace_id)::text, '')::uuid, NULLIF(sqlc.arg(actor_user_id)::text, '')::uuid,
        sqlc.arg(action), sqlc.arg(resource_type), NULLIF(sqlc.arg(resource_id)::text, ''), NULLIF(sqlc.arg(ip)::text, '')::inet,
        sqlc.arg(metadata)::jsonb, sqlc.arg(created_at)::timestamptz);

-- name: ListAudit :many
-- Newest first by (created_at, id). cursor_id 0 starts at the top; otherwise rows strictly older than
-- (cursor_at, cursor_id). action and workspace_id are filters when not empty.
SELECT a.id, a.created_at, a.action, a.resource_type, COALESCE(a.resource_id, '')::text AS resource_id,
       COALESCE(a.workspace_id::text, '')::text AS workspace_id, COALESCE(a.actor_user_id::text, '')::text AS actor_user_id,
       COALESCE(u.email, '')::text AS actor_email, COALESCE(u.name, '')::text AS actor_name,
       COALESCE(host(a.ip), '')::text AS ip, a.metadata
FROM audit_log a LEFT JOIN users u ON u.id = a.actor_user_id AND u.org_id = a.org_id
WHERE a.org_id = sqlc.arg(org_id)::text::uuid
  AND (sqlc.arg(action)::text = '' OR a.action = sqlc.arg(action)::text)
  AND (sqlc.arg(workspace_id)::text = '' OR a.workspace_id = NULLIF(sqlc.arg(workspace_id)::text, '')::uuid)
  AND (sqlc.arg(cursor_id)::bigint = 0 OR (a.created_at, a.id) < (sqlc.arg(cursor_at)::timestamptz, sqlc.arg(cursor_id)::bigint))
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg(max_rows)::int;
