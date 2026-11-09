-- name: UpsertUser :one
-- The lazy mirror: empty claims never overwrite what is stored, and a soft-deleted
-- user stays deleted.
INSERT INTO users (org_id, clerk_user_id, email, name, avatar_url)
VALUES (sqlc.arg(org_id)::text::uuid, sqlc.arg(clerk_user_id), lower(btrim(sqlc.arg(email))), sqlc.arg(name), sqlc.arg(avatar_url))
ON CONFLICT (clerk_user_id) DO UPDATE SET
    email = COALESCE(NULLIF(EXCLUDED.email, ''), users.email),
    name = COALESCE(NULLIF(EXCLUDED.name, ''), users.name),
    avatar_url = COALESCE(NULLIF(EXCLUDED.avatar_url, ''), users.avatar_url)
WHERE users.org_id = EXCLUDED.org_id
RETURNING id::text AS id, clerk_user_id, email, name, avatar_url, (deleted_at IS NOT NULL)::boolean AS deleted;

-- name: GetUserByClerkID :one
SELECT id::text AS id, clerk_user_id, email, name, avatar_url, (deleted_at IS NOT NULL)::boolean AS deleted
FROM users WHERE org_id = sqlc.arg(org_id)::text::uuid AND clerk_user_id = sqlc.arg(clerk_user_id);

-- name: SoftDeleteUser :one
UPDATE users SET deleted_at = now()
WHERE org_id = sqlc.arg(org_id)::text::uuid AND clerk_user_id = sqlc.arg(clerk_user_id) AND deleted_at IS NULL
RETURNING id::text AS id;

-- name: GetPreferences :one
SELECT preferences FROM users
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(user_id)::text::uuid;

-- name: MergePreferences :one
UPDATE users SET preferences = preferences || sqlc.arg(patch)::jsonb
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(user_id)::text::uuid
RETURNING preferences;
