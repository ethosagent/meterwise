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

-- name: SavingsCallSavers :many
-- Per UTC day of [from_ts, to_ts) plus one totals row: what trim and the router saved, read from calls by
-- started_at. An acted trim is saved (certain) or likely by trim_confidence; observe amounts live in the same
-- columns and are would-save. A routed call is saved (always certain); the router has no tokens. Fires count
-- the calls the feature recorded a decision on (a held route records nothing). No column adds two classes.
SELECT (started_at AT TIME ZONE 'UTC')::date AS day,
       (COALESCE(SUM(saved_usd_trim) FILTER (WHERE trim_action = 'acted' AND trim_confidence = 'certain'), 0) * 1000000)::bigint AS trim_saved_micro,
       (COALESCE(SUM(saved_usd_trim) FILTER (WHERE trim_action = 'acted' AND trim_confidence = 'likely'), 0) * 1000000)::bigint AS trim_likely_micro,
       (COALESCE(SUM(saved_usd_trim) FILTER (WHERE trim_action = 'observe'), 0) * 1000000)::bigint AS trim_would_micro,
       COALESCE(SUM(trim_tokens) FILTER (WHERE trim_action = 'acted' AND trim_confidence = 'certain'), 0)::bigint AS trim_saved_tokens,
       COALESCE(SUM(trim_tokens) FILTER (WHERE trim_action = 'acted' AND trim_confidence = 'likely'), 0)::bigint AS trim_likely_tokens,
       COALESCE(SUM(trim_tokens) FILTER (WHERE trim_action = 'observe'), 0)::bigint AS trim_would_tokens,
       COUNT(*) FILTER (WHERE trim_action IS NOT NULL)::bigint AS trim_fires,
       (COALESCE(SUM(saved_usd_router) FILTER (WHERE router_action = 'routed'), 0) * 1000000)::bigint AS router_saved_micro,
       (COALESCE(SUM(saved_usd_router) FILTER (WHERE router_action = 'observe'), 0) * 1000000)::bigint AS router_would_micro,
       COUNT(*) FILTER (WHERE router_action IS NOT NULL)::bigint AS router_fires
FROM calls
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND started_at >= sqlc.arg(from_ts)::timestamptz AND started_at < sqlc.arg(to_ts)::timestamptz
GROUP BY GROUPING SETS (((started_at AT TIME ZONE 'UTC')::date), ())
ORDER BY 1 NULLS LAST;

-- name: SavingsMemoryByDay :many
-- Per UTC day of [from_ts, to_ts) plus one totals row: what memory's injected hints are credited (likely only, by the
-- hit's ts: the credit is set at session close and joins no certain number) and how many hints were injected. Observe-only
-- hits carry no money. No column adds two classes.
SELECT (ts AT TIME ZONE 'UTC')::date AS day,
       (COALESCE(SUM(est_saved_usd) FILTER (WHERE injected), 0) * 1000000)::bigint AS likely_micro,
       COALESCE(SUM(est_saved_tokens) FILTER (WHERE injected), 0)::bigint AS likely_tokens,
       COUNT(*) FILTER (WHERE injected)::bigint AS fires
FROM memory_hits
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND ts >= sqlc.arg(from_ts)::timestamptz AND ts < sqlc.arg(to_ts)::timestamptz
GROUP BY GROUPING SETS (((ts AT TIME ZONE 'UTC')::date), ())
ORDER BY 1 NULLS LAST;

-- name: SavingsHasActed :one
-- Whether any saver has ever acted in the workspace (a monitor note or block, an acted trim, a routed call, an injected hint):
-- the collecting/active split.
SELECT (EXISTS (SELECT 1 FROM monitor_events WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND action <> 'observe')
        OR EXISTS (SELECT 1 FROM calls WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
                   AND (trim_action = 'acted' OR router_action = 'routed'))
        OR EXISTS (SELECT 1 FROM memory_hits WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND injected))::bool AS acted;

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
-- Per client (calls.client as stored; '' is NULL), one scan of [prev_from_ts, to_ts): calls, distinct sessions,
-- blocked calls and priced spend over [from_ts, to_ts), the calls and sessions of the window before it
-- ([prev_from_ts, from_ts)), for the busiest max_rows clients of the current window only, so a previous window
-- with many distinct clients cannot push a current client out of the cap.
SELECT COALESCE(client, '')::text AS client,
       (COUNT(*) FILTER (WHERE started_at >= sqlc.arg(from_ts)::timestamptz))::bigint AS calls,
       (COUNT(DISTINCT session_id) FILTER (WHERE started_at >= sqlc.arg(from_ts)::timestamptz))::bigint AS sessions,
       (COUNT(*) FILTER (WHERE blocked AND started_at >= sqlc.arg(from_ts)::timestamptz))::bigint AS blocked,
       (COALESCE(SUM(cost_usd) FILTER (WHERE started_at >= sqlc.arg(from_ts)::timestamptz), 0) * 1000000)::bigint AS spent_micro,
       (COUNT(*) FILTER (WHERE started_at < sqlc.arg(from_ts)::timestamptz))::bigint AS prev_calls,
       (COUNT(DISTINCT session_id) FILTER (WHERE started_at < sqlc.arg(from_ts)::timestamptz))::bigint AS prev_sessions
FROM calls
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND started_at >= sqlc.arg(prev_from_ts)::timestamptz AND started_at < sqlc.arg(to_ts)::timestamptz
GROUP BY 1
HAVING COUNT(*) FILTER (WHERE started_at >= sqlc.arg(from_ts)::timestamptz) > 0
ORDER BY 2 DESC, 1
LIMIT sqlc.arg(max_rows)::int;

-- name: SavingsClientSaved :many
-- Saved and likely money per client: the monitors' acted events through the call each belongs to (ruling S2
-- filters), plus the calls' own acted trim and routed money by started_at and memory's likely credit by the hit's ts,
-- so the clients add up to the hero.
SELECT client::text AS client, (SUM(saved_micro))::bigint AS saved_micro, (SUM(likely_micro))::bigint AS likely_micro
FROM (
    SELECT COALESCE(c.client, '') AS client,
           COALESCE(SUM(e.est_saved_usd) FILTER (WHERE e.confidence = 'certain'), 0) * 1000000 AS saved_micro,
           COALESCE(SUM(e.est_saved_usd) FILTER (WHERE e.confidence = 'likely'), 0) * 1000000 AS likely_micro
    FROM monitor_events e
    JOIN calls c ON c.id = e.call_id AND c.workspace_id = e.workspace_id
    WHERE e.workspace_id = sqlc.arg(workspace_id)::text::uuid AND e.action <> 'observe'
      AND e.ts >= sqlc.arg(from_ts)::timestamptz AND e.ts < sqlc.arg(to_ts)::timestamptz
    GROUP BY 1
    UNION ALL
    SELECT COALESCE(client, ''),
           COALESCE(SUM(saved_usd_trim) FILTER (WHERE trim_action = 'acted' AND trim_confidence = 'certain'), 0) * 1000000
             + COALESCE(SUM(saved_usd_router) FILTER (WHERE router_action = 'routed'), 0) * 1000000,
           COALESCE(SUM(saved_usd_trim) FILTER (WHERE trim_action = 'acted' AND trim_confidence = 'likely'), 0) * 1000000
    FROM calls
    WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
      AND started_at >= sqlc.arg(from_ts)::timestamptz AND started_at < sqlc.arg(to_ts)::timestamptz
    GROUP BY 1
    UNION ALL
    SELECT COALESCE(c.client, ''), 0, COALESCE(SUM(h.est_saved_usd), 0) * 1000000
    FROM memory_hits h
    JOIN calls c ON c.id = h.call_id AND c.workspace_id = h.workspace_id
    WHERE h.workspace_id = sqlc.arg(workspace_id)::text::uuid AND h.injected
      AND h.ts >= sqlc.arg(from_ts)::timestamptz AND h.ts < sqlc.arg(to_ts)::timestamptz
    GROUP BY 1
) u
GROUP BY 1;

-- name: ListSaves :many
-- Biggest saves: acted events with a positive estimate (either confidence), (estimate micro-USD DESC, id DESC),
-- keyset-paged. A NULL cursor is the first page: its bound is above every row.
SELECT e.id, e.session_id, e.call_id, e.call_index, e.ts, e.monitor, e.action, e.confidence,
       (e.est_saved_usd * 1000000)::bigint AS est_micro, e.est_saved_tokens,
       COALESCE(NULLIF(c.client, ''), '')::text AS client, COALESCE(c.model, '')::text AS model
FROM monitor_events e
LEFT JOIN calls c ON c.id = e.call_id AND c.workspace_id = e.workspace_id
WHERE e.workspace_id = sqlc.arg(workspace_id)::text::uuid AND e.action <> 'observe' AND e.est_saved_usd > 0
  AND e.ts >= sqlc.arg(from_ts)::timestamptz AND e.ts < sqlc.arg(to_ts)::timestamptz
  AND ((e.est_saved_usd * 1000000)::bigint, e.id) < (COALESCE(sqlc.narg(cursor_micro)::bigint, 9223372036854775807), COALESCE(sqlc.narg(cursor_id)::bigint, 9223372036854775807))
ORDER BY (e.est_saved_usd * 1000000)::bigint DESC, e.id DESC
LIMIT sqlc.arg(max_rows)::int;
