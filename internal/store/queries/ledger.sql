-- name: InsertCall :batchone
-- The only INSERT into calls (H4). Idempotent on the tollgate call id: a repeat inserts nothing and
-- returns no row, which is how the ledger knows not to count the call into its session twice.
-- cost_micro is NULL for a model with no price. Money converts from micro-USD to numeric(14,6) here.
INSERT INTO calls (id, org_id, workspace_id, session_id, call_index, started_at, provider, model, wire, stream, status, finish,
                   input_tokens, output_tokens, cached_tokens, reasoning_tokens, cost_usd, ttfb_ms, duration_ms, tool_calls,
                   blocked, notes_injected, client)
VALUES (sqlc.arg(id)::text, sqlc.arg(org_id)::text::uuid, sqlc.arg(workspace_id)::text::uuid, sqlc.arg(session_id)::text,
        sqlc.arg(call_index)::int, sqlc.arg(started_at)::timestamptz, sqlc.arg(provider)::text, sqlc.arg(model)::text,
        sqlc.arg(wire)::text, sqlc.arg(stream)::bool, sqlc.arg(status)::int, sqlc.arg(finish)::text,
        sqlc.arg(input_tokens)::bigint, sqlc.arg(output_tokens)::bigint, sqlc.arg(cached_tokens)::bigint,
        sqlc.arg(reasoning_tokens)::bigint, sqlc.narg(cost_micro)::bigint::numeric / 1000000, sqlc.arg(ttfb_ms)::int,
        sqlc.arg(duration_ms)::int, sqlc.arg(tool_calls)::jsonb, sqlc.arg(blocked)::bool, sqlc.arg(notes_injected)::int,
        NULLIF(sqlc.arg(client)::text, ''))
ON CONFLICT (id) DO NOTHING
RETURNING id;

-- name: UpsertSession :batchexec
-- The only INSERT into sessions (H4). The ledger passes the aggregate of the calls a batch really inserted
-- (never a repeat), so adding to the stored counters is exact. A newer call reopens a closed session.
INSERT INTO sessions (id, org_id, workspace_id, source, started_at, last_seen_at, calls, input_tokens, output_tokens, cost_usd,
                      client, first_user_message_preview, last_finish, last_had_text)
VALUES (sqlc.arg(id)::text, sqlc.arg(org_id)::text::uuid, sqlc.arg(workspace_id)::text::uuid, sqlc.arg(source)::text,
        sqlc.arg(started_at)::timestamptz, sqlc.arg(last_seen_at)::timestamptz, sqlc.arg(calls)::int,
        sqlc.arg(input_tokens)::bigint, sqlc.arg(output_tokens)::bigint, sqlc.narg(cost_micro)::bigint::numeric / 1000000,
        NULLIF(sqlc.arg(client)::text, ''), NULLIF(sqlc.arg(preview)::text, ''), sqlc.arg(last_finish)::text,
        sqlc.arg(last_had_text)::bool)
ON CONFLICT (workspace_id, id) DO UPDATE SET
    started_at = LEAST(sessions.started_at, EXCLUDED.started_at),
    calls = sessions.calls + EXCLUDED.calls,
    input_tokens = sessions.input_tokens + EXCLUDED.input_tokens,
    output_tokens = sessions.output_tokens + EXCLUDED.output_tokens,
    cost_usd = CASE WHEN sessions.cost_usd IS NULL AND EXCLUDED.cost_usd IS NULL THEN NULL
                    ELSE COALESCE(sessions.cost_usd, 0) + COALESCE(EXCLUDED.cost_usd, 0) END,
    client = COALESCE(sessions.client, EXCLUDED.client),
    first_user_message_preview = COALESCE(sessions.first_user_message_preview, EXCLUDED.first_user_message_preview),
    last_finish = CASE WHEN EXCLUDED.last_seen_at >= sessions.last_seen_at THEN EXCLUDED.last_finish ELSE sessions.last_finish END,
    last_had_text = CASE WHEN EXCLUDED.last_seen_at >= sessions.last_seen_at THEN EXCLUDED.last_had_text ELSE sessions.last_had_text END,
    closed_at = CASE WHEN EXCLUDED.last_seen_at > sessions.last_seen_at THEN NULL ELSE sessions.closed_at END,
    outcome = CASE WHEN EXCLUDED.last_seen_at > sessions.last_seen_at THEN 'open' ELSE sessions.outcome END,
    last_seen_at = GREATEST(sessions.last_seen_at, EXCLUDED.last_seen_at);

-- name: CloseSession :execrows
-- Finalise a quiet session: closed_at and the outcome heuristic (last finish ok and non-empty assistant
-- text is completed, anything else abandoned). An already closed session is left alone.
UPDATE sessions
SET closed_at = sqlc.arg(closed_at)::timestamptz,
    outcome = CASE WHEN last_finish = 'ok' AND last_had_text THEN 'completed' ELSE 'abandoned' END
WHERE workspace_id = sqlc.arg(workspace_id)::text::uuid AND id = sqlc.arg(id)::text AND closed_at IS NULL;

-- name: TouchWorkspaceLastCall :exec
-- One coalesced touch per workspace per batch; never moves last_call_at backwards.
UPDATE workspaces
SET last_call_at = GREATEST(COALESCE(last_call_at, sqlc.arg(at)::timestamptz), sqlc.arg(at)::timestamptz)
WHERE id = sqlc.arg(workspace_id)::text::uuid AND org_id = sqlc.arg(org_id)::text::uuid;
