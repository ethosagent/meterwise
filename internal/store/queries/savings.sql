-- Plan 07 savings read model. Every query is scoped by workspace_id. Money comes from monitor_events by ts
-- (the same filters as MonitorStats, ruling S2) and from calls.cost_usd by started_at. Days are UTC.

-- name: SavingsSpendByDay :many
-- Per UTC day of [from_ts, to_ts): priced spend, calls, distinct sessions and calls with no price, plus one
-- totals row (the row whose day is NULL; the empty grouping set always yields one).
SELECT (started_at AT TIME ZONE 'UTC')::date AS day, (COALESCE(SUM(cost_usd), 0) * 1000000)::bigint AS paid_micro,
       COUNT(*)::bigint AS calls, COUNT(DISTINCT session_id)::bigint AS sessions,
       COUNT(*) FILTER (WHERE cost_usd IS NULL)::bigint AS unpriced
FROM calls
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND started_at >= sqlc.arg(from_ts)::timestamptz AND started_at < sqlc.arg(to_ts)::timestamptz
GROUP BY GROUPING SETS (((started_at AT TIME ZONE 'UTC')::date), ())
ORDER BY 1 NULLS LAST;

-- name: SavingsEventsByDay :many
-- Per UTC day of [from_ts, to_ts): the three savings classes in money and tokens (saved = acted and certain,
-- likely = acted and likely, would-save = observe), plus one totals row. No column adds two classes.
SELECT (ts AT TIME ZONE 'UTC')::date AS day,
       (COALESCE(SUM(est_saved_usd) FILTER (WHERE action <> 'observe' AND confidence = 'certain'), 0) * 1000000)::bigint AS saved_micro,
       (COALESCE(SUM(est_saved_usd) FILTER (WHERE action <> 'observe' AND confidence = 'likely'), 0) * 1000000)::bigint AS likely_micro,
       (COALESCE(SUM(est_saved_usd) FILTER (WHERE action = 'observe'), 0) * 1000000)::bigint AS would_save_micro,
       COALESCE(SUM(est_saved_tokens) FILTER (WHERE action <> 'observe' AND confidence = 'certain'), 0)::bigint AS saved_tokens,
       COALESCE(SUM(est_saved_tokens) FILTER (WHERE action <> 'observe' AND confidence = 'likely'), 0)::bigint AS likely_saved_tokens,
       COALESCE(SUM(est_saved_tokens) FILTER (WHERE action = 'observe'), 0)::bigint AS would_save_tokens
FROM monitor_events
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND ts >= sqlc.arg(from_ts)::timestamptz AND ts < sqlc.arg(to_ts)::timestamptz
GROUP BY GROUPING SETS (((ts AT TIME ZONE 'UTC')::date), ())
ORDER BY 1 NULLS LAST;

-- name: SavingsHasActed :one
-- Whether any monitor has ever acted (note or block) in the workspace: the collecting/active split.
SELECT EXISTS (SELECT 1 FROM monitor_events WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND action <> 'observe')::bool AS acted;

-- name: SavingsCauseTops :many
-- For each monitor, the most frequent client, tool and path (rank 1 of each dimension) over the window. A
-- path dimension value is the stored path or, without one, its hash prefix; n is its number of fires.
WITH ev AS (
    SELECT e.monitor, COALESCE(s.client, '') AS client, COALESCE(e.evidence ->> 'tool', '') AS tool,
           COALESCE(e.evidence ->> 'path', '') AS path, COALESCE(e.evidence ->> 'pathHash', '') AS path_hash
    FROM monitor_events e
    LEFT JOIN sessions s ON s.workspace_id = e.workspace_id AND s.id = e.session_id
    WHERE e.workspace_id = sqlc.arg(workspace_id)::text::uuid
      AND e.ts >= sqlc.arg(from_ts)::timestamptz AND e.ts < sqlc.arg(to_ts)::timestamptz
), dims AS (
    SELECT monitor, 'client'::text AS dim, client AS val, ''::text AS val2 FROM ev WHERE client <> ''
    UNION ALL SELECT monitor, 'tool', tool, '' FROM ev WHERE tool <> ''
    UNION ALL SELECT monitor, 'path', path, path_hash FROM ev WHERE path <> '' OR path_hash <> ''
), counted AS (
    SELECT monitor, dim, val, val2, COUNT(*) AS n,
           ROW_NUMBER() OVER (PARTITION BY monitor, dim ORDER BY COUNT(*) DESC, val, val2) AS rn
    FROM dims GROUP BY monitor, dim, val, val2
)
SELECT monitor, dim, val, val2, n::bigint AS n FROM counted WHERE rn = 1;

-- name: SavingsRepeatTargets :many
-- The most repeated (monitor, tool, path, path hash) targets of the repetition monitors over the window.
SELECT monitor, COALESCE(evidence ->> 'tool', '')::text AS tool, COALESCE(evidence ->> 'path', '')::text AS path,
       COALESCE(evidence ->> 'pathHash', '')::text AS path_hash, COUNT(*)::bigint AS n
FROM monitor_events
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND ts >= sqlc.arg(from_ts)::timestamptz AND ts < sqlc.arg(to_ts)::timestamptz
  AND monitor IN ('duplicate_tool_call', 'repeated_read')
  AND COALESCE(evidence ->> 'tool', '') <> ''
GROUP BY 1, 2, 3, 4
ORDER BY n DESC, 1, 2, 3, 4
LIMIT sqlc.arg(max_rows)::int;

-- name: SavingsClients :many
-- Per client (calls.client as stored; '' is NULL) over [from_ts, to_ts): calls, distinct sessions, blocked
-- calls and priced spend; the busiest max_rows clients.
SELECT COALESCE(client, '')::text AS client, COUNT(*)::bigint AS calls, COUNT(DISTINCT session_id)::bigint AS sessions,
       COUNT(*) FILTER (WHERE blocked)::bigint AS blocked, (COALESCE(SUM(cost_usd), 0) * 1000000)::bigint AS spent_micro
FROM calls
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND started_at >= sqlc.arg(from_ts)::timestamptz AND started_at < sqlc.arg(to_ts)::timestamptz
GROUP BY 1
ORDER BY 2 DESC, 1
LIMIT sqlc.arg(max_rows)::int;

-- name: SavingsClientsFor :many
-- The same per-client counts for exactly the named clients ('' is NULL), so a previous window with many
-- distinct clients cannot push a current client out of a row cap.
SELECT COALESCE(client, '')::text AS client, COUNT(*)::bigint AS calls, COUNT(DISTINCT session_id)::bigint AS sessions,
       COUNT(*) FILTER (WHERE blocked)::bigint AS blocked, (COALESCE(SUM(cost_usd), 0) * 1000000)::bigint AS spent_micro
FROM calls
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND started_at >= sqlc.arg(from_ts)::timestamptz AND started_at < sqlc.arg(to_ts)::timestamptz
  AND COALESCE(client, '') = ANY(sqlc.arg(clients)::text[])
GROUP BY 1;

-- name: SavingsClientSaved :many
-- Saved and likely money per client of the call each acted event belongs to (ruling S2 filters).
SELECT COALESCE(c.client, '')::text AS client,
       (COALESCE(SUM(e.est_saved_usd) FILTER (WHERE e.confidence = 'certain'), 0) * 1000000)::bigint AS saved_micro,
       (COALESCE(SUM(e.est_saved_usd) FILTER (WHERE e.confidence = 'likely'), 0) * 1000000)::bigint AS likely_micro
FROM monitor_events e
JOIN calls c ON c.id = e.call_id AND c.workspace_id = e.workspace_id
WHERE e.workspace_id = sqlc.arg(workspace_id)::text::uuid AND e.action <> 'observe'
  AND e.ts >= sqlc.arg(from_ts)::timestamptz AND e.ts < sqlc.arg(to_ts)::timestamptz
GROUP BY 1;

-- name: ListSaves :many
-- Biggest saves: acted events with a positive estimate (either confidence), (estimate micro-USD DESC, id DESC),
-- keyset-paged. A NULL cursor is the first page: its bound is above every row.
SELECT e.id, e.session_id, e.call_id, e.call_index, e.ts, e.monitor, e.action, e.confidence,
       (e.est_saved_usd * 1000000)::bigint AS est_micro, e.est_saved_tokens,
       NULLIF(c.client, '')::text AS client, COALESCE(c.model, '')::text AS model
FROM monitor_events e
LEFT JOIN calls c ON c.id = e.call_id AND c.workspace_id = e.workspace_id
WHERE e.workspace_id = sqlc.arg(workspace_id)::text::uuid AND e.action <> 'observe' AND e.est_saved_usd > 0
  AND e.ts >= sqlc.arg(from_ts)::timestamptz AND e.ts < sqlc.arg(to_ts)::timestamptz
  AND ((e.est_saved_usd * 1000000)::bigint, e.id) < (COALESCE(sqlc.narg(cursor_micro)::bigint, 9223372036854775807), COALESCE(sqlc.narg(cursor_id)::bigint, 9223372036854775807))
ORDER BY (e.est_saved_usd * 1000000)::bigint DESC, e.id DESC
LIMIT sqlc.arg(max_rows)::int;
