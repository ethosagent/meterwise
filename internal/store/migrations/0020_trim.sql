-- feature: trim
-- +goose Up
-- Plan 11 (step 0): one row per non-reverted trim stage of a call, written by internal/ledger (the only
-- writer, H4) in the call's transaction. Sizes and token estimates only; no request text is stored. The unique
-- key makes a replayed call_id add nothing (06 R9).
CREATE TABLE trim_events (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id       uuid NOT NULL REFERENCES organizations (id),
    workspace_id uuid NOT NULL,
    session_id   text NOT NULL,
    call_id      text NOT NULL,
    ts           timestamptz NOT NULL,
    stage        text NOT NULL CHECK (stage IN ('repeat_collapse', 'log_window', 'dup_lines', 'json_table', 'schema_trim')),
    tool_name    text,
    regions      integer NOT NULL,
    bytes_before integer NOT NULL,
    bytes_after  integer NOT NULL,
    est_tokens   bigint NOT NULL DEFAULT 0,
    reverted     boolean NOT NULL,
    action       text NOT NULL CHECK (action IN ('acted', 'observe')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, call_id, stage)
);

CREATE INDEX trim_events_workspace_ts_idx ON trim_events (workspace_id, ts DESC);

-- +goose Down
DROP TABLE trim_events;
