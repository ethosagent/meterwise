-- feature: calls
-- +goose Up
-- One row per completed proxied call (Plan 05). internal/ledger is the only writer (H4) and writes
-- idempotently on id (the tollgate call id). Metadata only: no message text lives here (privacy
-- mode). workspace_id and session_id carry no foreign key on purpose: a row is written after the
-- fact, in batches, and outlives the workspace like audit_log. cost_usd is NULL for a model with no
-- price (flagged, never a silent 0). Money is numeric(14,6) USD (ruling R12).
CREATE TABLE calls (
    id               text PRIMARY KEY CHECK (id <> ''),
    org_id           uuid NOT NULL REFERENCES organizations (id),
    workspace_id     uuid NOT NULL,
    session_id       text NOT NULL,
    call_index       integer NOT NULL CHECK (call_index >= 0),
    started_at       timestamptz NOT NULL,
    provider         text NOT NULL,
    model            text NOT NULL,
    wire             text NOT NULL,
    stream           boolean NOT NULL,
    status           integer NOT NULL,
    finish           text NOT NULL,
    input_tokens     bigint NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens    bigint NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cached_tokens    bigint NOT NULL DEFAULT 0 CHECK (cached_tokens >= 0),
    reasoning_tokens bigint NOT NULL DEFAULT 0 CHECK (reasoning_tokens >= 0),
    cost_usd         numeric(14,6),
    ttfb_ms          integer NOT NULL DEFAULT 0,
    duration_ms      integer NOT NULL DEFAULT 0,
    tool_calls       jsonb NOT NULL DEFAULT '[]'::jsonb,
    blocked          boolean NOT NULL DEFAULT false,
    notes_injected   integer NOT NULL DEFAULT 0,
    client           text,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX calls_workspace_started_idx ON calls (workspace_id, started_at DESC);
CREATE INDEX calls_workspace_session_idx ON calls (workspace_id, session_id, call_index);

-- +goose Down
DROP TABLE calls;
