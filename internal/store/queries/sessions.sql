-- name: ListSessions :many
-- One page of a workspace's sessions, newest first by (started_at, id); [from_ts, to_ts) bounds started_at.
-- A NULL cursor is the first page: the keyset bound becomes ('infinity', ''), which every finite row is below,
-- so the planner still sees one row-comparison range on the (started_at, id) order.
SELECT id, workspace_id::text AS workspace_id, source, started_at, last_seen_at, closed_at, calls, input_tokens, output_tokens,
       COALESCE((cost_usd * 1000000)::bigint, 0)::bigint AS cost_micro, (cost_usd IS NOT NULL)::bool AS has_cost, (saved_usd * 1000000)::bigint AS saved_micro,
       (likely_saved_usd * 1000000)::bigint AS likely_saved_micro, outcome, COALESCE(client, '')::text AS client,
       trim_tokens, routed_calls, hints
FROM sessions
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND started_at >= sqlc.arg(from_ts)::timestamptz AND started_at < sqlc.arg(to_ts)::timestamptz
  AND (started_at, id) < (COALESCE(sqlc.narg(cursor_at)::timestamptz, 'infinity'::timestamptz), COALESCE(sqlc.narg(cursor_id)::text, ''))
  AND (sqlc.narg(client)::text IS NULL OR COALESCE(client, 'unknown') = sqlc.narg(client)::text)
  AND (sqlc.narg(outcome)::text IS NULL OR outcome = sqlc.narg(outcome)::text)
  AND (sqlc.narg(tag)::text IS NULL
       OR (sqlc.narg(tag)::text = 'trimmed' AND trim_tokens > 0)
       OR (sqlc.narg(tag)::text = 'routed' AND routed_calls > 0)
       OR (sqlc.narg(tag)::text = 'remembered' AND hints > 0))
ORDER BY started_at DESC, id DESC
LIMIT sqlc.arg(max_rows)::int;

-- name: GetSession :one
SELECT id, workspace_id::text AS workspace_id, source, started_at, last_seen_at, closed_at, calls, input_tokens, output_tokens,
       COALESCE((cost_usd * 1000000)::bigint, 0)::bigint AS cost_micro, (cost_usd IS NOT NULL)::bool AS has_cost, (saved_usd * 1000000)::bigint AS saved_micro,
       (likely_saved_usd * 1000000)::bigint AS likely_saved_micro, outcome, COALESCE(client, '')::text AS client,
       trim_tokens, routed_calls, hints
FROM sessions
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND id = sqlc.arg(id)::text;

-- name: ListSessionCalls :many
SELECT id, call_index, started_at, provider, model, wire, stream, status, finish, input_tokens, output_tokens, cached_tokens,
       reasoning_tokens, COALESCE((cost_usd * 1000000)::bigint, 0)::bigint AS cost_micro, (cost_usd IS NOT NULL)::bool AS has_cost, ttfb_ms, duration_ms, tool_calls, blocked, notes_injected,
       trim_action, trim_confidence, trim_tokens, (saved_usd_trim * 1000000)::bigint AS saved_trim_micro,
       router_action, routed_from, routed_to, router_score_bp, (saved_usd_router * 1000000)::bigint AS saved_router_micro, hint_injected
FROM calls
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND session_id = sqlc.arg(session_id)::text
ORDER BY call_index, started_at, id
LIMIT sqlc.arg(max_rows)::int;

-- name: Stats :many
-- Per UTC day of [from_ts, to_ts) (spend over priced calls only, calls, distinct sessions) plus one totals row,
-- the row whose day is NULL (the empty grouping set always yields a row, even for no calls).
SELECT (started_at AT TIME ZONE 'UTC')::date AS day, (COALESCE(SUM(cost_usd), 0) * 1000000)::bigint AS spend_micro,
       COUNT(*)::bigint AS calls, COUNT(DISTINCT session_id)::bigint AS sessions
FROM calls
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND started_at >= sqlc.arg(from_ts)::timestamptz AND started_at < sqlc.arg(to_ts)::timestamptz
GROUP BY GROUPING SETS (((started_at AT TIME ZONE 'UTC')::date), ())
ORDER BY 1 NULLS LAST;

-- name: CallStatus :one
-- Setup.status: the first call ever and today's count (since since_ts, the UTC day start).
SELECT (SELECT MIN(started_at) FROM calls WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid)::timestamptz AS first_call_at,
       (SELECT COUNT(*) FROM calls WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
                                     AND started_at >= sqlc.arg(since_ts)::timestamptz)::bigint AS calls_today;
