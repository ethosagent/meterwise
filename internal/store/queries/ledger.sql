-- name: InsertCalls :many
-- The only INSERT into calls (H4). One statement for a whole batch (a JSON array of call objects),
-- idempotent on the tollgate call id: a repeat inserts nothing and returns no id, which is how the
-- ledger knows not to count the call into its session twice. A missing "cost" is NULL (a model with no
-- price). Money converts from micro-USD to numeric(14,6) here; "startedUs" is unix microseconds. The saver fields
-- (plans 09-11) are NULL or 0 for a call no saver touched: "trimAction"/"trimConfidence"/"routerAction" ''/missing is
-- NULL; "savedTrim" and "savedRouter" are micro-USD of the action's class (certain or likely money when it acted,
-- would-save when it only observed).
INSERT INTO calls (id, org_id, workspace_id, session_id, call_index, started_at, provider, model, wire, stream, status, finish,
                   input_tokens, output_tokens, cached_tokens, reasoning_tokens, cost_usd, ttfb_ms, duration_ms, tool_calls,
                   blocked, notes_injected, client, trim_action, trim_confidence, trim_tokens, saved_usd_trim, router_action,
                   routed_from, routed_to, router_score_bp, saved_usd_router, hint_injected)
SELECT r.j->>'id', (r.j->>'orgId')::uuid, (r.j->>'workspaceId')::uuid, r.j->>'sessionId', (r.j->>'callIndex')::int,
       timestamptz 'epoch' + (r.j->>'startedUs')::bigint * interval '1 microsecond', r.j->>'provider', r.j->>'model', r.j->>'wire',
       (r.j->>'stream')::bool, (r.j->>'status')::int, r.j->>'finish', (r.j->>'input')::bigint, (r.j->>'output')::bigint,
       (r.j->>'cached')::bigint, (r.j->>'reasoning')::bigint, (r.j->>'cost')::numeric / 1000000, (r.j->>'ttfbMs')::int,
       (r.j->>'durationMs')::int, COALESCE(r.j->'toolCalls', '[]'::jsonb), (r.j->>'blocked')::bool, (r.j->>'notesInjected')::int, NULLIF(r.j->>'client', ''),
       NULLIF(r.j->>'trimAction', ''), NULLIF(r.j->>'trimConfidence', ''), COALESCE((r.j->>'trimTokens')::bigint, 0),
       COALESCE((r.j->>'savedTrim')::numeric, 0) / 1000000, NULLIF(r.j->>'routerAction', ''), NULLIF(r.j->>'routedFrom', ''),
       NULLIF(r.j->>'routedTo', ''), (r.j->>'routerScoreBp')::int, COALESCE((r.j->>'savedRouter')::numeric, 0) / 1000000,
       COALESCE((r.j->>'hintInjected')::bool, false)
FROM jsonb_array_elements(sqlc.arg(calls_json)::jsonb) AS r(j)
ON CONFLICT (id) DO NOTHING
RETURNING id;

-- name: UpsertSessions :exec
-- The only INSERT into sessions (H4), one statement for every session of a batch (a JSON array with one object per
-- session, sorted so concurrent batches lock rows in one order). The ledger passes the aggregate of the calls a
-- batch really inserted (never a repeat), so adding to the stored counters is exact. A newer call reopens a closed
-- session. started_at is the keyset sort key of the session list: set by the first insert, never changed. A missing
-- "cost" is NULL (all calls unpriced). "saved" and "likely" are the two savings numbers of the inserted calls (monitor
-- events that acted, by confidence; never summed, never observe). Money converts from micro-USD to numeric(14,6)
-- here; times are unix microseconds. "trimTokens", "routedCalls" and "hints" are the saver totals of the inserted calls;
-- "saved" and "likely" already include acted trim (by confidence) and routed savings.
INSERT INTO sessions (id, org_id, workspace_id, source, started_at, last_seen_at, calls, input_tokens, output_tokens, cost_usd,
                      client, first_user_message_preview, last_finish, last_had_text, saved_usd, likely_saved_usd,
                      trim_tokens, routed_calls, hints)
SELECT r.j->>'id', (r.j->>'orgId')::uuid, (r.j->>'workspaceId')::uuid, r.j->>'source',
       timestamptz 'epoch' + (r.j->>'startedUs')::bigint * interval '1 microsecond',
       timestamptz 'epoch' + (r.j->>'lastSeenUs')::bigint * interval '1 microsecond',
       (r.j->>'calls')::int, (r.j->>'input')::bigint, (r.j->>'output')::bigint, (r.j->>'cost')::numeric / 1000000,
       NULLIF(r.j->>'client', ''), NULLIF(r.j->>'preview', ''), r.j->>'lastFinish', (r.j->>'lastHadText')::bool,
       (r.j->>'saved')::numeric / 1000000, (r.j->>'likely')::numeric / 1000000,
       COALESCE((r.j->>'trimTokens')::bigint, 0), COALESCE((r.j->>'routedCalls')::int, 0), COALESCE((r.j->>'hints')::int, 0)
FROM jsonb_array_elements(sqlc.arg(sessions_json)::jsonb) AS r(j)
ON CONFLICT (workspace_id, id) DO UPDATE SET
    calls = sessions.calls + EXCLUDED.calls,
    input_tokens = sessions.input_tokens + EXCLUDED.input_tokens,
    output_tokens = sessions.output_tokens + EXCLUDED.output_tokens,
    saved_usd = sessions.saved_usd + EXCLUDED.saved_usd,
    likely_saved_usd = sessions.likely_saved_usd + EXCLUDED.likely_saved_usd,
    trim_tokens = sessions.trim_tokens + EXCLUDED.trim_tokens,
    routed_calls = sessions.routed_calls + EXCLUDED.routed_calls,
    hints = sessions.hints + EXCLUDED.hints,
    cost_usd = CASE WHEN sessions.cost_usd IS NULL AND EXCLUDED.cost_usd IS NULL THEN NULL
                    ELSE COALESCE(sessions.cost_usd, 0) + COALESCE(EXCLUDED.cost_usd, 0) END,
    client = COALESCE(sessions.client, EXCLUDED.client),
    first_user_message_preview = COALESCE(sessions.first_user_message_preview, EXCLUDED.first_user_message_preview),
    last_finish = CASE WHEN EXCLUDED.last_seen_at >= sessions.last_seen_at THEN EXCLUDED.last_finish ELSE sessions.last_finish END,
    last_had_text = CASE WHEN EXCLUDED.last_seen_at >= sessions.last_seen_at THEN EXCLUDED.last_had_text ELSE sessions.last_had_text END,
    closed_at = CASE WHEN EXCLUDED.last_seen_at > sessions.last_seen_at THEN NULL ELSE sessions.closed_at END,
    outcome = CASE WHEN EXCLUDED.last_seen_at > sessions.last_seen_at THEN 'open' ELSE sessions.outcome END,
    last_seen_at = GREATEST(sessions.last_seen_at, EXCLUDED.last_seen_at);

-- name: CloseSessions :many
-- Finalise quiet sessions, one statement per flush (a JSON array of {workspaceId, id, atUs}): closed_at and the
-- outcome heuristic (last finish ok and non-empty assistant text is completed, anything else abandoned). An already
-- closed session is left alone. RETURNING names the sessions this statement really closed, with their outcome and
-- call count: the memory feature learns from them (Options.OnClosed), once per close.
UPDATE sessions s
SET closed_at = timestamptz 'epoch' + (t.j->>'atUs')::bigint * interval '1 microsecond',
    outcome = CASE WHEN s.last_finish = 'ok' AND s.last_had_text THEN 'completed' ELSE 'abandoned' END
FROM jsonb_array_elements(sqlc.arg(closes_json)::jsonb) AS t(j)
WHERE s.workspace_id = (t.j->>'workspaceId')::uuid AND s.id = t.j->>'id' AND s.closed_at IS NULL
RETURNING s.workspace_id::text AS workspace_id, s.id, s.outcome, s.calls;

-- name: TouchWorkspacesLastCall :exec
-- One statement per batch (a JSON array of {workspaceId, orgId, atUs}): each workspace's last_call_at moves to its
-- newest call end, and only forward (a batch of older calls writes nothing).
UPDATE workspaces w
SET last_call_at = timestamptz 'epoch' + (t.j->>'atUs')::bigint * interval '1 microsecond'
FROM jsonb_array_elements(sqlc.arg(touches_json)::jsonb) AS t(j)
WHERE w.id = (t.j->>'workspaceId')::uuid AND w.org_id = (t.j->>'orgId')::uuid
  AND (w.last_call_at IS NULL OR w.last_call_at < timestamptz 'epoch' + (t.j->>'atUs')::bigint * interval '1 microsecond');

-- name: InsertMonitorEvents :many
-- The only INSERT into monitor_events (H4): one statement for the verdicts of the calls a batch really inserted (a
-- JSON array of event objects), idempotent on (workspace_id, call_id, monitor). ts is the call's start;
-- "estMicro" is micro-USD, converted to numeric(14,6) here. RETURNING names the rows really inserted: the
-- session sums count only those (a duplicate (call, monitor) is dropped here and so is never summed).
INSERT INTO monitor_events (org_id, workspace_id, session_id, call_id, call_index, ts, monitor, mode, action, evidence,
                            est_saved_tokens, est_saved_usd, confidence)
SELECT (r.j->>'orgId')::uuid, (r.j->>'workspaceId')::uuid, r.j->>'sessionId', r.j->>'callId', (r.j->>'callIndex')::int,
       timestamptz 'epoch' + (r.j->>'startedUs')::bigint * interval '1 microsecond', r.j->>'monitor', r.j->>'mode', r.j->>'action',
       r.j->'evidence', (r.j->>'estTokens')::bigint, (r.j->>'estMicro')::numeric / 1000000, r.j->>'confidence'
FROM jsonb_array_elements(sqlc.arg(events_json)::jsonb) AS r(j)
ON CONFLICT (workspace_id, call_id, monitor) DO NOTHING
RETURNING call_id, monitor;

-- name: SetMonitorEventFeedback :one
-- The only UPDATE of monitor_events (H4): feedback on one event of one workspace; a foreign or unknown id matches no
-- row (the store reports not_found). A NULL feedback clears it.
UPDATE monitor_events
SET feedback = sqlc.narg(feedback)::text
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND id = sqlc.arg(id)::bigint
RETURNING id, org_id::text AS org_id, workspace_id::text AS workspace_id, session_id, call_id, call_index, ts, monitor, mode, action,
          evidence, est_saved_tokens, (est_saved_usd * 1000000)::bigint AS est_saved_micro, confidence, feedback, created_at;

-- name: InsertTrimEvents :exec
-- The only INSERT into trim_events (H4): one row per non-reverted trim stage of the calls a batch really inserted (a JSON
-- array of stage objects), idempotent on (workspace_id, call_id, stage). ts is the call's start; sizes and the token
-- estimate only, never request text.
INSERT INTO trim_events (org_id, workspace_id, session_id, call_id, ts, stage, tool_name, regions, bytes_before, bytes_after,
                         est_tokens, reverted, action)
SELECT (r.j->>'orgId')::uuid, (r.j->>'workspaceId')::uuid, r.j->>'sessionId', r.j->>'callId',
       timestamptz 'epoch' + (r.j->>'startedUs')::bigint * interval '1 microsecond', r.j->>'stage', NULLIF(r.j->>'tool', ''),
       (r.j->>'regions')::int, (r.j->>'bytesBefore')::int, (r.j->>'bytesAfter')::int, (r.j->>'estTokens')::bigint,
       (r.j->>'reverted')::bool, r.j->>'action'
FROM jsonb_array_elements(sqlc.arg(events_json)::jsonb) AS r(j)
ON CONFLICT (workspace_id, call_id, stage) DO NOTHING;

-- name: InsertMemoryHits :many
-- The only INSERT into memory_hits (H4): one row per recall decision of the calls a batch really inserted (a JSON array
-- of hit objects), idempotent on (workspace_id, call_id). The WHERE EXISTS keeps a hit from outliving a call the batch
-- did not write. ts is the call's start; no hint text is stored (it is re-rendered from the trace).
INSERT INTO memory_hits (org_id, workspace_id, session_id, call_id, trace_id, similarity_bp, mode, injected, ts)
SELECT (r.j->>'orgId')::uuid, (r.j->>'workspaceId')::uuid, r.j->>'sessionId', r.j->>'callId', (r.j->>'traceId')::bigint,
       (r.j->>'similarityBp')::int, r.j->>'mode', (r.j->>'injected')::bool,
       timestamptz 'epoch' + (r.j->>'startedUs')::bigint * interval '1 microsecond'
FROM jsonb_array_elements(sqlc.arg(hits_json)::jsonb) AS r(j)
WHERE EXISTS (SELECT 1 FROM calls c WHERE c.workspace_id = (r.j->>'workspaceId')::uuid AND c.id = r.j->>'callId')
ON CONFLICT (workspace_id, call_id) DO NOTHING
RETURNING id;

-- name: SetMemoryHitFeedback :one
-- The only UPDATE of a hit's feedback (H4): one hit of one workspace; a foreign or unknown id matches no row (the
-- store reports not_found). A NULL feedback clears it.
UPDATE memory_hits
SET feedback = sqlc.narg(feedback)::text
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND id = sqlc.arg(id)::bigint
RETURNING id, trace_id;

-- name: CreditMemoryHit :one
-- The likely credit of one injected hit, set once at session close: it only applies while the hit has none.
UPDATE memory_hits
SET est_saved_tokens = sqlc.arg(tokens)::bigint, est_saved_usd = sqlc.arg(usd_micro)::bigint / 1000000.0
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND id = sqlc.arg(id)::bigint AND session_id = sqlc.arg(session_id)::text
  AND injected AND est_saved_usd = 0 AND est_saved_tokens = 0
RETURNING id;

-- name: AddSessionLikelySaved :exec
-- Memory's likely credit joins the session's likely number (never the certain one).
UPDATE sessions
SET likely_saved_usd = likely_saved_usd + sqlc.arg(usd_micro)::bigint / 1000000.0
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND id = sqlc.arg(id)::text;
