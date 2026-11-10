-- feature: calls
-- +goose Up
-- Plans 09-11 step 0: what trimming, routing and memory did to a call, written by internal/ledger (the only
-- writer, H4) in the call's transaction. A NULL action means the feature did not touch the call. trim_confidence
-- says how sure trim_tokens/saved_usd_trim are (certain only for OpenAI chat with reported input tokens, R8).
-- saved_usd_* are certain-or-likely money per trim_confidence and certain money for the router; never summed with
-- each other's classes (D11). routed_from is the requested model; the call's model column is the routed target.
ALTER TABLE calls
    ADD COLUMN trim_action text CHECK (trim_action IN ('acted', 'observe')),
    ADD COLUMN trim_confidence text CHECK (trim_confidence IN ('certain', 'likely')),
    ADD COLUMN trim_tokens bigint NOT NULL DEFAULT 0 CHECK (trim_tokens >= 0),
    ADD COLUMN saved_usd_trim numeric(14,6) NOT NULL DEFAULT 0 CHECK (saved_usd_trim >= 0),
    ADD COLUMN router_action text CHECK (router_action IN ('routed', 'observe')),
    ADD COLUMN routed_from text,
    ADD COLUMN routed_to text,
    ADD COLUMN router_score_bp integer CHECK (router_score_bp BETWEEN 0 AND 10000),
    ADD COLUMN saved_usd_router numeric(14,6) NOT NULL DEFAULT 0 CHECK (saved_usd_router >= 0),
    ADD COLUMN hint_injected boolean NOT NULL DEFAULT false;

CREATE INDEX calls_workspace_routed_idx ON calls (workspace_id, started_at DESC) WHERE router_action IS NOT NULL;

-- +goose Down
DROP INDEX calls_workspace_routed_idx;
ALTER TABLE calls
    DROP COLUMN hint_injected,
    DROP COLUMN saved_usd_router,
    DROP COLUMN router_score_bp,
    DROP COLUMN routed_to,
    DROP COLUMN routed_from,
    DROP COLUMN router_action,
    DROP COLUMN saved_usd_trim,
    DROP COLUMN trim_tokens,
    DROP COLUMN trim_confidence,
    DROP COLUMN trim_action;
