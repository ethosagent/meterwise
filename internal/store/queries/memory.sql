-- Plan 09 memory: traces, failures, the self-usage budget and the read side of hits. Every query is scoped by
-- workspace_id (or org_id for the one retention sweep). Vectors cross the driver as text literals cast ::vector
-- (ADR 0031). memory_hits is a ledger table (H4): it is only READ here; internal/ledger (queries/ledger.sql) writes it.

-- name: InsertMemoryTrace :one
-- One trace per (workspace, session): a replayed close inserts nothing and returns no row. embedding is the literal
-- '[0.1,0.2,...]'; the column is vector(1536).
INSERT INTO memory_traces (org_id, workspace_id, session_id, task_summary, observations, approach, fix, signature,
                           base_confidence, distil_prompt_v, embed_model, embedding)
VALUES (sqlc.arg(org_id)::text::uuid, sqlc.arg(workspace_id)::text::uuid, sqlc.arg(session_id)::text, sqlc.arg(task_summary)::text,
        sqlc.arg(observations)::jsonb, sqlc.arg(approach)::text, sqlc.arg(fix)::text, sqlc.arg(signature)::jsonb,
        sqlc.arg(base_confidence)::real, sqlc.arg(distil_prompt_v)::int, sqlc.arg(embed_model)::text,
        sqlc.arg(embedding)::text::vector)
ON CONFLICT (workspace_id, session_id) DO NOTHING
RETURNING id;

-- name: TouchMemoryTrace :exec
-- A hint was injected from the trace.
UPDATE memory_traces SET uses = uses + 1, last_used_at = now()
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND id = sqlc.arg(id)::bigint;

-- name: RecountMemoryTraceVotes :one
-- Recompute the votes of one trace from its hits' feedback; the generated confidence column follows. Returns the
-- confidence in basis points.
UPDATE memory_traces t
SET upvotes = v.up, downvotes = v.down
FROM (SELECT COUNT(*) FILTER (WHERE h.feedback = 'helpful')::int AS up, COUNT(*) FILTER (WHERE h.feedback = 'irrelevant')::int AS down
      FROM memory_hits h
      WHERE h.workspace_id = sqlc.arg(workspace_id)::text::uuid AND h.trace_id = sqlc.arg(id)::bigint) v
WHERE t.workspace_id = sqlc.arg(workspace_id)::text::uuid AND t.id = sqlc.arg(id)::bigint
RETURNING (t.confidence * 10000)::int AS confidence_bp;

-- name: ListMemoryTraces :many
-- One page of the workspace's traces, newest first by (created_at, id); a NULL cursor is the first page.
SELECT id, session_id::text AS session_id, task_summary, signature, uses, upvotes, downvotes,
       (confidence * 10000)::int AS confidence_bp, last_used_at, created_at
FROM memory_traces
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND (created_at, id) < (COALESCE(sqlc.narg(cursor_at)::timestamptz, 'infinity'::timestamptz), COALESCE(sqlc.narg(cursor_id)::bigint, 9223372036854775807))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(max_rows)::int;

-- name: GetMemoryTrace :one
SELECT id, session_id::text AS session_id, task_summary, observations, approach, fix, signature, uses, upvotes, downvotes,
       (confidence * 10000)::int AS confidence_bp, last_used_at, created_at
FROM memory_traces
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND id = sqlc.arg(id)::bigint;

-- name: ListTraceHelpedSessions :many
-- The sessions a trace was injected into, newest first (the caller caps at 50).
SELECT session_id::text AS session_id
FROM memory_hits
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND trace_id = sqlc.arg(trace_id)::bigint AND injected
GROUP BY session_id
ORDER BY MAX(ts) DESC, session_id
LIMIT sqlc.arg(max_rows)::int;

-- name: DeleteWorkspaceMemoryTraces :one
-- Forget memory: every trace and failure record of the workspace. Returns how many traces went.
WITH failures AS (
    DELETE FROM memory_trace_failures WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
), traces AS (
    DELETE FROM memory_traces WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid RETURNING 1
)
SELECT COUNT(*)::bigint AS removed FROM traces;

-- name: PruneMemory :one
-- Retention sweep for the org: traces not used (or, never used, not created) since the cutoff, and failure records
-- older than it. Returns the traces removed.
WITH failures AS (
    DELETE FROM memory_trace_failures WHERE org_id = sqlc.arg(org_id)::text::uuid AND created_at < sqlc.arg(cutoff)::timestamptz
), traces AS (
    DELETE FROM memory_traces
    WHERE org_id = sqlc.arg(org_id)::text::uuid AND COALESCE(last_used_at, created_at) < sqlc.arg(cutoff)::timestamptz
    RETURNING 1
)
SELECT COUNT(*)::bigint AS removed FROM traces;

-- name: InsertMemoryTraceFailure :exec
INSERT INTO memory_trace_failures (org_id, workspace_id, session_id, signature, last_error, calls, cost_usd)
VALUES (sqlc.arg(org_id)::text::uuid, sqlc.arg(workspace_id)::text::uuid, sqlc.arg(session_id)::text, sqlc.arg(signature)::jsonb,
        sqlc.arg(last_error)::text, sqlc.arg(calls)::int, sqlc.narg(cost_micro)::bigint / 1000000.0)
ON CONFLICT (workspace_id, session_id) DO NOTHING;

-- name: InsertMemorySelfUsage :exec
INSERT INTO memory_self_usage (org_id, workspace_id, kind, model, input_tokens, output_tokens, cost_usd)
VALUES (sqlc.arg(org_id)::text::uuid, sqlc.arg(workspace_id)::text::uuid, sqlc.arg(kind)::text, sqlc.arg(model)::text,
        sqlc.arg(input_tokens)::bigint, sqlc.arg(output_tokens)::bigint, sqlc.arg(cost_micro)::bigint / 1000000.0);

-- name: MemorySelfUsageSince :one
-- Memory's own model spend of the workspace since since_ts, in micro-USD.
SELECT (COALESCE(SUM(cost_usd), 0) * 1000000)::bigint AS cost_micro
FROM memory_self_usage
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND created_at >= sqlc.arg(since_ts)::timestamptz;

-- name: MemoryWorkspaceCapture :one
-- The capture and privacy settings of one workspace of the org (Status reads them; a missing key is NULL).
SELECT COALESCE(settings ->> 'capture', '')::text AS capture, COALESCE(settings ->> 'privacyMode', '')::text AS privacy_mode
FROM workspaces
WHERE org_id = sqlc.arg(org_id)::text::uuid AND id = sqlc.arg(id)::text::uuid;

-- name: CountMemoryTraces :one
SELECT COUNT(*)::bigint AS traces FROM memory_traces WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid;

-- name: MemoryHitStats :one
-- The hits of [from_ts, to_ts) by ts: injected hits (hits), observe-only hits, the sessions that got a hint, the
-- feedback counts and the likely money credited at session close. No column adds two classes.
SELECT COUNT(*) FILTER (WHERE injected)::bigint AS hits,
       COUNT(*) FILTER (WHERE NOT injected)::bigint AS observed_hits,
       COUNT(DISTINCT session_id) FILTER (WHERE injected)::bigint AS hinted_sessions,
       COUNT(*) FILTER (WHERE feedback = 'helpful')::bigint AS helpful,
       COUNT(*) FILTER (WHERE feedback = 'irrelevant')::bigint AS irrelevant,
       (COALESCE(SUM(est_saved_usd) FILTER (WHERE injected), 0) * 1000000)::bigint AS likely_micro
FROM memory_hits
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid
  AND ts >= sqlc.arg(from_ts)::timestamptz AND ts < sqlc.arg(to_ts)::timestamptz;

-- name: ListSessionMemoryHits :many
-- The memory hits of one session with what is needed to re-render the hint (the hint text is never stored). A trace
-- that was forgotten leaves its hit with an empty source and no text.
SELECT h.id, h.call_id, h.trace_id, h.similarity_bp, h.mode, h.injected, h.feedback,
       (h.est_saved_usd * 1000000)::bigint AS est_saved_micro,
       COALESCE(t.session_id, '')::text AS source_session_id, COALESCE(t.approach, '')::text AS approach,
       COALESCE(t.fix, '')::text AS fix, COALESCE(t.signature, '{}'::jsonb) AS signature
FROM memory_hits h
LEFT JOIN memory_traces t ON t.workspace_id = h.workspace_id AND t.id = h.trace_id
WHERE h.workspace_id = sqlc.arg(workspace_id)::text::uuid AND h.session_id = sqlc.arg(session_id)::text
ORDER BY h.ts, h.id
LIMIT sqlc.arg(max_rows)::int;

-- name: GetMemoryHit :one
SELECT h.id, h.call_id, h.trace_id, h.similarity_bp, h.mode, h.injected, h.feedback,
       (h.est_saved_usd * 1000000)::bigint AS est_saved_micro,
       COALESCE(t.session_id, '')::text AS source_session_id, COALESCE(t.approach, '')::text AS approach,
       COALESCE(t.fix, '')::text AS fix, COALESCE(t.signature, '{}'::jsonb) AS signature
FROM memory_hits h
LEFT JOIN memory_traces t ON t.workspace_id = h.workspace_id AND t.id = h.trace_id
WHERE h.workspace_id = sqlc.arg(workspace_id)::text::uuid AND h.id = sqlc.arg(id)::bigint;

-- name: ListSessionInjectedHits :many
-- The injected hits of a session that have no credit yet (the likely credit is set once, at close).
SELECT id, trace_id FROM memory_hits
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND session_id = sqlc.arg(session_id)::text AND injected
  AND est_saved_usd = 0 AND est_saved_tokens = 0
ORDER BY ts, id;

-- name: MemoryBaseline :one
-- What a completed, un-hinted session of the workspace looked like over the last 30 days (excluding this session):
-- how many, their median call count and their per-call tokens and cost. The likely credit compares a hinted session
-- with it.
SELECT COUNT(*)::bigint AS sessions,
       COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY calls), 0)::float8 AS median_calls,
       COALESCE(SUM(calls), 0)::bigint AS calls,
       COALESCE(SUM(input_tokens + output_tokens), 0)::bigint AS tokens,
       (COALESCE(SUM(cost_usd), 0) * 1000000)::bigint AS cost_micro
FROM sessions
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND outcome = 'completed' AND hints = 0
  AND id <> sqlc.arg(session_id)::text AND closed_at >= sqlc.arg(since_ts)::timestamptz;
