-- Plan 11 trim read port (stats and the per-call preview). Every query is scoped by workspace_id. Money and tokens
-- come from calls by started_at (the same columns and filters as SavingsCallSavers, so the Overview trim row and the
-- hero agree); the per-stage and per-tool tables come from trim_events by ts. Days are UTC. Nothing here writes: the
-- ledger is the one writer of calls and trim_events (H4), and no request body is stored anywhere.

-- name: TrimCallStats :one
-- Totals over [from_ts, to_ts): calls, calls whose body was rewritten, and the three classes (certain, likely,
-- would-save) in money and tokens. No column adds two classes.
SELECT COUNT(*)::bigint AS calls,
       COUNT(*) FILTER (WHERE trim_action = 'acted')::bigint AS trimmed_calls,
       (COALESCE(SUM(saved_usd_trim) FILTER (WHERE trim_action = 'acted' AND trim_confidence = 'certain'), 0) * 1000000)::bigint AS saved_micro,
       (COALESCE(SUM(saved_usd_trim) FILTER (WHERE trim_action = 'acted' AND trim_confidence = 'likely'), 0) * 1000000)::bigint AS likely_micro,
       (COALESCE(SUM(saved_usd_trim) FILTER (WHERE trim_action = 'observe'), 0) * 1000000)::bigint AS would_micro,
       COALESCE(SUM(trim_tokens) FILTER (WHERE trim_action = 'acted'), 0)::bigint AS trim_tokens,
       COALESCE(SUM(trim_tokens) FILTER (WHERE trim_action = 'observe'), 0)::bigint AS would_tokens
FROM calls
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND started_at >= sqlc.arg(from_ts)::timestamptz AND started_at < sqlc.arg(to_ts)::timestamptz;

-- name: TrimStageStats :many
-- Per stage over [from_ts, to_ts), calls that acted only: how often it fired, how many fires were reverted (the
-- ledger records only non-reverted stages, so this is 0 until it does), the tokens and the bytes it saved.
SELECT stage, COUNT(*)::bigint AS fires, COUNT(*) FILTER (WHERE reverted)::bigint AS reverted,
       COALESCE(SUM(est_tokens), 0)::bigint AS tokens, COALESCE(SUM(bytes_before - bytes_after), 0)::bigint AS bytes_saved
FROM trim_events
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND action = 'acted'
  AND ts >= sqlc.arg(from_ts)::timestamptz AND ts < sqlc.arg(to_ts)::timestamptz
GROUP BY stage;

-- name: TrimToolStats :many
-- Per tool over [from_ts, to_ts), calls that acted only, most tokens first, at most max_rows. A stage with no single
-- tool reads "unknown".
SELECT COALESCE(NULLIF(tool_name, ''), 'unknown')::text AS tool, COUNT(*)::bigint AS fires, COALESCE(SUM(est_tokens), 0)::bigint AS tokens
FROM trim_events
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND action = 'acted'
  AND ts >= sqlc.arg(from_ts)::timestamptz AND ts < sqlc.arg(to_ts)::timestamptz
GROUP BY 1
ORDER BY tokens DESC, tool
LIMIT sqlc.arg(max_rows)::int;

-- name: TrimCallAction :one
-- The trim action of one call of the workspace (NULL when the call was not trimmed); no row when the call is not the
-- workspace's.
SELECT trim_action
FROM calls
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND id = sqlc.arg(call_id)::text;

-- name: TrimCallEvents :many
-- The stages of one call: sizes and token estimates only, in the fixed stage order.
SELECT stage, tool_name, regions, bytes_before, bytes_after, est_tokens, reverted, action
FROM trim_events
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND call_id = sqlc.arg(call_id)::text
ORDER BY CASE stage WHEN 'repeat_collapse' THEN 1 WHEN 'log_window' THEN 2 WHEN 'dup_lines' THEN 3 WHEN 'json_table' THEN 4 ELSE 5 END;
