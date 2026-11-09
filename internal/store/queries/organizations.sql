-- name: GetOrganization :one
SELECT id::text AS id, name FROM organizations WHERE id = sqlc.arg(org_id)::text::uuid;
