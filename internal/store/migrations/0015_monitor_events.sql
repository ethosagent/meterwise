-- feature: monitor_events
-- +goose Up
-- What a monitor decided about a call (Plan 06 section 4). Created empty by Plan 05 so the dashboard
-- can query it before any monitor exists; internal/ledger is the only writer (H4). est_saved_usd is
-- numeric(14,6) USD (ruling R12). The two savings numbers (certain, likely) are told apart by confidence.
CREATE TABLE monitor_events (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id           uuid NOT NULL REFERENCES organizations (id),
    workspace_id     uuid NOT NULL,
    session_id       text NOT NULL,
    call_id          text NOT NULL,
    call_index       integer NOT NULL CHECK (call_index >= 0),
    ts               timestamptz NOT NULL,
    monitor          text NOT NULL CHECK (monitor <> ''),
    mode             text NOT NULL CHECK (mode IN ('observe', 'note', 'block')),
    action           text NOT NULL CHECK (action IN ('observe', 'note', 'block')),
    evidence         jsonb NOT NULL DEFAULT '{}'::jsonb,
    est_saved_tokens bigint NOT NULL DEFAULT 0 CHECK (est_saved_tokens >= 0),
    est_saved_usd    numeric(14,6) NOT NULL DEFAULT 0,
    confidence       text NOT NULL CHECK (confidence IN ('certain', 'likely', 'none')),
    feedback         text CHECK (feedback IN ('correct', 'wrong')),
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX monitor_events_workspace_ts_idx ON monitor_events (workspace_id, ts DESC);
CREATE INDEX monitor_events_session_idx ON monitor_events (session_id);

-- +goose Down
DROP TABLE monitor_events;
