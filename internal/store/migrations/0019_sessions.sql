-- feature: sessions
-- +goose Up
-- Plans 09-11 step 0: per-session totals of what the savers did, aggregated by internal/ledger with the rest of
-- the session upsert. The router's escalate_until stays in memory only, never a column.
ALTER TABLE sessions
    ADD COLUMN trim_tokens bigint NOT NULL DEFAULT 0,
    ADD COLUMN routed_calls integer NOT NULL DEFAULT 0,
    ADD COLUMN hints integer NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE sessions
    DROP COLUMN hints,
    DROP COLUMN routed_calls,
    DROP COLUMN trim_tokens;
