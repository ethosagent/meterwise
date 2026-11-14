-- feature: memory
-- +goose Up
-- Plan 09 (the trace library): distilled solutions of finished sessions, embedded with pgvector, recalled per
-- workspace as a one-line hint. pgvector is not a "trusted" extension (pg_available_extension_versions.trusted is
-- false), so a non-superuser migrate role cannot create it: a superuser runs `CREATE EXTENSION vector` once per
-- database before the first migrate (docs/runbooks/database-roles.md). With the extension already there the
-- statement below is a no-op and needs no privilege. Vectors cross the driver as text literals cast ::vector in SQL
-- (ADR 0031), so the Go side has no vector dependency.
CREATE EXTENSION IF NOT EXISTS vector;

-- sqlc maps a column of type vector to the Go type pgvector.Vector (a module this repo does not import, ADR 0031), so
-- the column is declared through a domain: sqlc sees an opaque type, the database sees vector(1536). Casts to ::vector
-- in queries/memory.sql do the rest.
CREATE DOMAIN memory_embedding AS vector(1536);

-- One row per distilled session. No credential and no prompt text beyond what the distiller returned lives here
-- (the redaction pass runs before the insert). (workspace_id, session_id) is unique: a replayed close adds nothing.
-- confidence is the distiller's own (base_confidence) moved by feedback: three downvotes and no upvote demote it
-- by 0.2, three upvotes promote it by 0.2; a generated column keeps it exact whatever order the feedback arrives in.
CREATE TABLE memory_traces (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id          uuid NOT NULL REFERENCES organizations (id),
    workspace_id    uuid NOT NULL,
    session_id      text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    task_summary    text NOT NULL,
    observations    jsonb NOT NULL DEFAULT '[]'::jsonb,
    approach        text NOT NULL,
    fix             text NOT NULL,
    signature       jsonb NOT NULL DEFAULT '{}'::jsonb,
    base_confidence real NOT NULL CHECK (base_confidence >= 0 AND base_confidence <= 1),
    confidence      real GENERATED ALWAYS AS (
        LEAST(1::real, GREATEST(0::real, base_confidence + CASE
            WHEN upvotes >= 3 THEN 0.2::real
            WHEN downvotes >= 3 AND upvotes = 0 THEN -0.2::real
            ELSE 0::real END))
    ) STORED,
    distil_prompt_v integer NOT NULL,
    embed_model     text NOT NULL,
    embedding       memory_embedding NOT NULL,
    uses            integer NOT NULL DEFAULT 0 CHECK (uses >= 0),
    upvotes         integer NOT NULL DEFAULT 0 CHECK (upvotes >= 0),
    downvotes       integer NOT NULL DEFAULT 0 CHECK (downvotes >= 0),
    last_used_at    timestamptz,
    UNIQUE (workspace_id, session_id)
);

-- pgvector reads the dimension from the column's own type and cannot see through the domain, so the index is on a
-- cast; the recall query orders by the same expression (store/memory_recall.go) and so uses it. The cast is halfvec
-- (16-bit floats): the graph is half the size of a vector(1536) one, so more of it stays in the buffer cache, and
-- cosine order barely changes. The stored column and the reported similarity stay full precision (ADR 0031).
CREATE INDEX memory_traces_embedding_idx ON memory_traces USING hnsw ((embedding::halfvec(1536)) halfvec_cosine_ops);
CREATE INDEX memory_traces_workspace_confidence_idx ON memory_traces (workspace_id, confidence);
CREATE INDEX memory_traces_workspace_created_idx ON memory_traces (workspace_id, created_at DESC, id DESC);

-- A session that did not finish with a solution: signature and last error only, no distil call, never recalled
-- (the Phase-2 analysis loop reads it later).
CREATE TABLE memory_trace_failures (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id          uuid NOT NULL REFERENCES organizations (id),
    workspace_id    uuid NOT NULL,
    session_id      text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    signature       jsonb NOT NULL DEFAULT '{}'::jsonb,
    last_error      text NOT NULL DEFAULT '',
    monitor_summary jsonb NOT NULL DEFAULT '{}'::jsonb,
    calls           integer NOT NULL DEFAULT 0 CHECK (calls >= 0),
    cost_usd        numeric(14,6),
    UNIQUE (workspace_id, session_id)
);

-- One row per recall decision of a call (a ledger table: internal/ledger is the only writer, H4). The hint text is
-- never stored; it is re-rendered from the trace. est_saved_* is the likely credit set at session close and stays 0
-- until then. trace_id has no foreign key on purpose: Forget memory deletes traces and a hit outlives them.
CREATE TABLE memory_hits (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id          uuid NOT NULL REFERENCES organizations (id),
    workspace_id    uuid NOT NULL,
    session_id      text NOT NULL,
    call_id         text NOT NULL,
    trace_id        bigint NOT NULL,
    similarity_bp   integer NOT NULL CHECK (similarity_bp >= 0 AND similarity_bp <= 10000),
    mode            text NOT NULL CHECK (mode IN ('observe', 'hint')),
    injected        boolean NOT NULL,
    feedback        text CHECK (feedback IN ('helpful', 'irrelevant')),
    est_saved_tokens bigint NOT NULL DEFAULT 0 CHECK (est_saved_tokens >= 0),
    est_saved_usd   numeric(14,6) NOT NULL DEFAULT 0 CHECK (est_saved_usd >= 0),
    ts              timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, call_id)
);

CREATE INDEX memory_hits_workspace_ts_idx ON memory_hits (workspace_id, ts DESC);
CREATE INDEX memory_hits_workspace_session_idx ON memory_hits (workspace_id, session_id);
CREATE INDEX memory_hits_workspace_trace_idx ON memory_hits (workspace_id, trace_id);

-- Memory's own model spend (the distil and embed calls), kept apart from the customer's ledger: it is our cost
-- (ADR 0032). The daily budget per workspace reads the sum of one UTC day.
CREATE TABLE memory_self_usage (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id        uuid NOT NULL REFERENCES organizations (id),
    workspace_id  uuid NOT NULL,
    kind          text NOT NULL CHECK (kind IN ('distil', 'embed')),
    model         text NOT NULL,
    input_tokens  bigint NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens bigint NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cost_usd      numeric(14,6) NOT NULL DEFAULT 0 CHECK (cost_usd >= 0),
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX memory_self_usage_workspace_created_idx ON memory_self_usage (workspace_id, created_at DESC);

-- +goose Down
-- The tables go; the extension goes with them so the schema is exactly what it was before Up (the migrate gate
-- compares pg_dump output). Dropping an extension needs its owner or a superuser: a non-superuser migrate role
-- that wants to roll this back asks its DBA (the runbook says so).
DROP TABLE memory_self_usage;
DROP TABLE memory_hits;
DROP TABLE memory_trace_failures;
DROP TABLE memory_traces;
DROP DOMAIN memory_embedding;
DROP EXTENSION IF EXISTS vector;
