-- name: ListSessionMonitorEvents :many
-- A session's monitor events in call order, at most max_rows. Scoped by workspace_id.
SELECT id, org_id::text AS org_id, workspace_id::text AS workspace_id, session_id, call_id, call_index, ts, monitor, mode, action,
       evidence, est_saved_tokens, (est_saved_usd * 1000000)::bigint AS est_saved_micro, confidence, feedback, created_at
FROM monitor_events
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND session_id = sqlc.arg(session_id)::text
ORDER BY call_index, id
LIMIT sqlc.arg(max_rows)::int;

-- name: MonitorStats :many
-- Per monitor over [from_ts, to_ts): fires, actions, and the three money columns. An event that acted (note or block)
-- is saved (confidence certain) or likely (confidence likely); an observe event is would-save only. The columns are
-- never summed into one number. Feedback counts are over the same events.
SELECT monitor, COUNT(*)::bigint AS fires,
       COUNT(*) FILTER (WHERE action = 'block')::bigint AS blocked,
       COUNT(*) FILTER (WHERE action = 'note')::bigint AS noted,
       COUNT(*) FILTER (WHERE action = 'observe')::bigint AS observed,
       COALESCE(SUM(est_saved_tokens) FILTER (WHERE action <> 'observe'), 0)::bigint AS est_saved_tokens,
       (COALESCE(SUM(est_saved_usd) FILTER (WHERE action <> 'observe' AND confidence = 'certain'), 0) * 1000000)::bigint AS saved_micro,
       (COALESCE(SUM(est_saved_usd) FILTER (WHERE action <> 'observe' AND confidence = 'likely'), 0) * 1000000)::bigint AS likely_micro,
       (COALESCE(SUM(est_saved_usd) FILTER (WHERE action = 'observe'), 0) * 1000000)::bigint AS would_save_micro,
       COUNT(*) FILTER (WHERE feedback = 'correct')::bigint AS feedback_correct,
       COUNT(*) FILTER (WHERE feedback = 'wrong')::bigint AS feedback_wrong
FROM monitor_events
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND ts >= sqlc.arg(from_ts)::timestamptz AND ts < sqlc.arg(to_ts)::timestamptz
GROUP BY monitor;

-- name: MonitorSavedTotals :one
-- The two savings numbers of a window (stats view): events that acted, by confidence. Never summed.
SELECT (COALESCE(SUM(est_saved_usd) FILTER (WHERE confidence = 'certain'), 0) * 1000000)::bigint AS saved_micro,
       (COALESCE(SUM(est_saved_usd) FILTER (WHERE confidence = 'likely'), 0) * 1000000)::bigint AS likely_micro
FROM monitor_events
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND action <> 'observe'
  AND ts >= sqlc.arg(from_ts)::timestamptz AND ts < sqlc.arg(to_ts)::timestamptz;
