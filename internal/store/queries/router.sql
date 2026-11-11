-- Plan 10 router read model. The route and capability tables are global (the shipped defaults); everything else is
-- scoped by workspace_id. This file only reads calls, sessions and monitor_events (the ledger writes them, H4);
-- router_feedback is the one table the control plane writes here.

-- name: RouterRoutes :many
-- The default route pairs, in a stable order.
SELECT provider, from_model, to_model
FROM router_model_routes
ORDER BY provider, from_model;

-- name: RouterCaps :many
-- The newest capability row per (provider, model) that is already effective.
SELECT DISTINCT ON (provider, model) provider, model, max_tools, supports_json_schema, supports_vision, supports_reasoning
FROM router_model_capabilities
WHERE effective_from <= now()
ORDER BY provider, model, effective_from DESC;

-- name: RouterSeenModels :many
-- The (provider, model) pairs the workspace's calls used in the last 30 days, busiest first; a routed call counts for
-- the model it asked for, not the cheaper one it ran on.
SELECT provider, COALESCE(routed_from, model)::text AS model
FROM calls
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND started_at >= now() - interval '30 days'
GROUP BY provider, COALESCE(routed_from, model)
ORDER BY COUNT(*) DESC, provider, COALESCE(routed_from, model)
LIMIT 200;

-- name: RouterCallStats :one
-- One row over [from_ts, to_ts): routed and observed calls, what they saved, and the guardrail counts. Cheap calls are
-- the routed ones the upstream answered (status under 400); strong calls are every other answered call. A call "fired"
-- when any monitor recorded an event on it. Unavailable counts routed calls the upstream refused (400, 403, 404).
-- A blocked call never reached an upstream and has no answer to compare, so it is left out of every count here (the
-- routed share's denominator, the strong baseline and the cheap calls alike).
SELECT COUNT(*)::bigint AS calls,
       COUNT(*) FILTER (WHERE router_action = 'routed')::bigint AS routed,
       COUNT(*) FILTER (WHERE router_action = 'observe')::bigint AS observed,
       COUNT(*) FILTER (WHERE router_action = 'routed' AND status IN (400, 403, 404))::bigint AS unavailable,
       (COALESCE(SUM(saved_usd_router) FILTER (WHERE router_action = 'routed'), 0) * 1000000)::bigint AS saved_micro,
       (COALESCE(SUM(saved_usd_router) FILTER (WHERE router_action = 'observe'), 0) * 1000000)::bigint AS would_micro,
       COUNT(*) FILTER (WHERE router_action = 'routed' AND status < 400)::bigint AS cheap_calls,
       COUNT(*) FILTER (WHERE router_action = 'routed' AND status < 400 AND fired)::bigint AS cheap_fired,
       COUNT(*) FILTER (WHERE router_action IS DISTINCT FROM 'routed' AND status < 400)::bigint AS strong_calls,
       COUNT(*) FILTER (WHERE router_action IS DISTINCT FROM 'routed' AND status < 400 AND fired)::bigint AS strong_fired
FROM (
    SELECT c.router_action, c.status, c.saved_usd_router,
           EXISTS (SELECT 1 FROM monitor_events m WHERE m.workspace_id = c.workspace_id AND m.call_id = c.id) AS fired
    FROM calls c
    WHERE c.workspace_id = sqlc.arg(workspace_id)::text::uuid AND NOT c.blocked
      AND c.started_at >= sqlc.arg(from_ts)::timestamptz AND c.started_at < sqlc.arg(to_ts)::timestamptz
) x;

-- name: RouterSessionStats :one
-- Closed sessions that started in [from_ts, to_ts), split by whether the router routed any of their calls, and how
-- many of each completed. An open session has no outcome yet and is left out of both.
SELECT COUNT(*) FILTER (WHERE routed_calls > 0 AND outcome <> 'open')::bigint AS cheap_closed,
       COUNT(*) FILTER (WHERE routed_calls > 0 AND outcome = 'completed')::bigint AS cheap_completed,
       COUNT(*) FILTER (WHERE routed_calls = 0 AND outcome <> 'open')::bigint AS strong_closed,
       COUNT(*) FILTER (WHERE routed_calls = 0 AND outcome = 'completed')::bigint AS strong_completed
FROM sessions
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND started_at >= sqlc.arg(from_ts)::timestamptz AND started_at < sqlc.arg(to_ts)::timestamptz;

-- name: RouterPairStats :many
-- Routed calls and their saving per (provider, requested model, target model) over the range.
SELECT provider, routed_from::text AS from_model, routed_to::text AS to_model, COUNT(*)::bigint AS calls,
       (COALESCE(SUM(saved_usd_router), 0) * 1000000)::bigint AS saved_micro
FROM calls
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND router_action = 'routed'
  AND routed_from IS NOT NULL AND routed_to IS NOT NULL
  AND started_at >= sqlc.arg(from_ts)::timestamptz AND started_at < sqlc.arg(to_ts)::timestamptz
GROUP BY provider, routed_from, routed_to
ORDER BY SUM(saved_usd_router) DESC, provider, routed_from, routed_to
LIMIT 50;

-- name: RouterWorseCount :one
-- Calls of the range a person marked "worse".
SELECT COUNT(*)::bigint AS worse
FROM router_feedback f
JOIN calls c ON c.workspace_id = f.workspace_id AND c.id = f.call_id
WHERE f.workspace_id = sqlc.arg(workspace_id)::text::uuid AND f.verdict = 'worse'
  AND c.started_at >= sqlc.arg(from_ts)::timestamptz AND c.started_at < sqlc.arg(to_ts)::timestamptz;

-- name: RouterCallExists :one
SELECT EXISTS (SELECT 1 FROM calls WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND id = sqlc.arg(call_id)::text)::bool AS found;

-- name: RouterFeedbackUpsert :one
-- Sets the verdict on a call of the workspace (the caller has checked the call exists there).
INSERT INTO router_feedback (workspace_id, call_id, verdict, user_id)
VALUES (sqlc.arg(workspace_id)::text::uuid, sqlc.arg(call_id)::text, sqlc.arg(verdict)::text, sqlc.narg(user_id)::text::uuid)
ON CONFLICT (workspace_id, call_id) DO UPDATE
SET verdict = EXCLUDED.verdict, user_id = EXCLUDED.user_id, updated_at = now()
RETURNING verdict;

-- name: RouterFeedbackDelete :exec
DELETE FROM router_feedback WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND call_id = sqlc.arg(call_id)::text;
