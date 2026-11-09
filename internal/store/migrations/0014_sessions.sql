-- feature: sessions
-- +goose Up
-- One row per agent run (Plan 05), aggregated by internal/ledger (the only writer, H4). The key is
-- (workspace_id, id) so a predicted session id can never collide across workspaces. last_finish and
-- last_had_text hold the last call's facts for the outcome heuristic at close: finish ok and
-- non-empty assistant text is completed, anything else abandoned. cost_usd is NULL until a call of
-- the session was priced. saved_usd and likely_saved_usd are the two savings numbers, never summed.
-- first_user_message_preview is NULL unless the workspace opted in to capture and privacy mode is off.
CREATE TABLE sessions (
    id                         text NOT NULL CHECK (id <> '' ),
    org_id                     uuid NOT NULL REFERENCES organizations (id),
    workspace_id               uuid NOT NULL,
    source                     text NOT NULL CHECK (source IN ('header', 'predicted')),
    started_at                 timestamptz NOT NULL,
    last_seen_at               timestamptz NOT NULL,
    closed_at                  timestamptz,
    calls                      integer NOT NULL DEFAULT 0,
    input_tokens               bigint NOT NULL DEFAULT 0,
    output_tokens              bigint NOT NULL DEFAULT 0,
    cost_usd                   numeric(14,6),
    saved_usd                  numeric(14,6) NOT NULL DEFAULT 0,
    likely_saved_usd           numeric(14,6) NOT NULL DEFAULT 0,
    outcome                    text NOT NULL DEFAULT 'open' CHECK (outcome IN ('completed', 'abandoned', 'open')),
    client                     text,
    first_user_message_preview text CHECK (length(first_user_message_preview) <= 200),
    last_finish                text NOT NULL DEFAULT '',
    last_had_text              boolean NOT NULL DEFAULT false,
    PRIMARY KEY (workspace_id, id)
);

CREATE INDEX sessions_workspace_started_idx ON sessions (workspace_id, started_at DESC, id DESC);

-- +goose Down
DROP TABLE sessions;
