-- name: ListSessionsFirst :many
-- First page of a workspace's sessions, newest first by (started_at, id); [from_ts, to_ts) bounds started_at.
SELECT id, workspace_id::text AS workspace_id, source, started_at, last_seen_at, closed_at, calls, input_tokens, output_tokens,
       (cost_usd * 1000000)::bigint AS cost_micro, (saved_usd * 1000000)::bigint AS saved_micro,
       (likely_saved_usd * 1000000)::bigint AS likely_saved_micro, outcome, COALESCE(client, '')::text AS client
FROM sessions
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND started_at >= sqlc.arg(from_ts)::timestamptz AND started_at < sqlc.arg(to_ts)::timestamptz
ORDER BY started_at DESC, id DESC
LIMIT sqlc.arg(max_rows)::int;

-- name: ListSessionsAfter :many
-- The page after the keyset (cursor_at, cursor_id): rows strictly older in (started_at, id) order.
SELECT id, workspace_id::text AS workspace_id, source, started_at, last_seen_at, closed_at, calls, input_tokens, output_tokens,
       (cost_usd * 1000000)::bigint AS cost_micro, (saved_usd * 1000000)::bigint AS saved_micro,
       (likely_saved_usd * 1000000)::bigint AS likely_saved_micro, outcome, COALESCE(client, '')::text AS client
FROM sessions
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND started_at >= sqlc.arg(from_ts)::timestamptz AND started_at < sqlc.arg(to_ts)::timestamptz
  AND (started_at, id) < (sqlc.arg(cursor_at)::timestamptz, sqlc.arg(cursor_id)::text)
ORDER BY started_at DESC, id DESC
LIMIT sqlc.arg(max_rows)::int;

-- name: GetSession :one
SELECT id, workspace_id::text AS workspace_id, source, started_at, last_seen_at, closed_at, calls, input_tokens, output_tokens,
       (cost_usd * 1000000)::bigint AS cost_micro, (saved_usd * 1000000)::bigint AS saved_micro,
       (likely_saved_usd * 1000000)::bigint AS likely_saved_micro, outcome, COALESCE(client, '')::text AS client
FROM sessions
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND id = sqlc.arg(id)::text;

-- name: ListSessionCalls :many
SELECT id, call_index, started_at, provider, model, wire, stream, status, finish, input_tokens, output_tokens, cached_tokens,
       reasoning_tokens, (cost_usd * 1000000)::bigint AS cost_micro, ttfb_ms, duration_ms, tool_calls, blocked, notes_injected
FROM calls
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND session_id = sqlc.arg(session_id)::text
ORDER BY call_index, started_at, id
LIMIT sqlc.arg(max_rows)::int;

-- name: StatsByDay :many
-- Per UTC day of [from_ts, to_ts): spend (priced calls only), calls and distinct sessions.
SELECT (started_at AT TIME ZONE 'UTC')::date AS day, (COALESCE(SUM(cost_usd), 0) * 1000000)::bigint AS spend_micro,
       COUNT(*)::bigint AS calls, COUNT(DISTINCT session_id)::bigint AS sessions
FROM calls
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND started_at >= sqlc.arg(from_ts)::timestamptz AND started_at < sqlc.arg(to_ts)::timestamptz
GROUP BY 1
ORDER BY 1;

-- name: StatsTotals :one
SELECT (COALESCE(SUM(cost_usd), 0) * 1000000)::bigint AS spend_micro, COUNT(*)::bigint AS calls,
       COUNT(DISTINCT session_id)::bigint AS sessions
FROM calls
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND started_at >= sqlc.arg(from_ts)::timestamptz AND started_at < sqlc.arg(to_ts)::timestamptz;

-- name: CallStatus :one
-- Setup.status: the first call ever and today's count (since since_ts, the UTC day start).
SELECT (SELECT MIN(started_at) FROM calls WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid)::timestamptz AS first_call_at,
       (SELECT COUNT(*) FROM calls WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
                                     AND started_at >= sqlc.arg(since_ts)::timestamptz)::bigint AS calls_today;
