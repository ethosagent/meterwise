-- feature: router
-- +goose Up
-- Plan 10 (step 0): router feedback, the verified model capabilities and the default route map.
-- router_model_capabilities: one row per (provider, model) target, effective from a time. max_tools is NULL when the
-- provider's page supports tool use and states no maximum (the router applies its own cap, a code constant), and 0 when
-- no tool definition may be sent (a request with any tool is not routed). A capability the provider's page does not
-- state is false (deny). Every row below carries the official page it was read from and the date it was fetched (A3).
-- router_model_routes: the default pairs (copied into a workspace on edit, R10). Both ends are priced in 0012/0022.
CREATE TABLE router_feedback (
    workspace_id uuid NOT NULL,
    call_id      text NOT NULL,
    verdict      text NOT NULL CHECK (verdict IN ('worse', 'fine')),
    user_id      uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, call_id)
);

CREATE TABLE router_model_capabilities (
    provider             text NOT NULL CHECK (provider <> ''),
    model                text NOT NULL CHECK (model <> ''),
    max_tools            integer CHECK (max_tools >= 0),
    supports_json_schema boolean NOT NULL,
    supports_vision      boolean NOT NULL,
    supports_reasoning   boolean NOT NULL,
    effective_from       timestamptz NOT NULL,
    PRIMARY KEY (provider, model, effective_from)
);

CREATE TABLE router_model_routes (
    provider   text NOT NULL CHECK (provider <> ''),
    from_model text NOT NULL CHECK (from_model <> ''),
    to_model   text NOT NULL CHECK (to_model <> ''),
    PRIMARY KEY (provider, from_model)
);

-- source: https://developers.openai.com/api/docs/models/gpt-5-mini, fetched 2026-11-11: function_calling,
--   structured_outputs and image_input are listed under supported features; the model details list reasoning token support.
-- source: https://developers.openai.com/api/docs/models/gpt-4.1-mini, fetched 2026-11-11: function_calling,
--   structured_outputs and image_input are listed; the page says "low latency without a reasoning step", so reasoning is false.
-- source: https://developers.openai.com/api/docs/models/gpt-4o-mini, fetched 2026-11-11: function_calling,
--   structured_outputs and image_input are listed; reasoning is not stated, so false.
-- source: https://platform.claude.com/docs/en/models/sonnet-5-5/overview and
--   https://platform.claude.com/docs/en/about-claude/models/overview, fetched 2026-11-11: "All current models support text
--   and image input ... vision, and tool use"; Thinking: Adaptive; no structured outputs / JSON schema statement, so false.
-- source: https://platform.claude.com/docs/en/models/haiku-4-5/overview, fetched 2026-11-11: Input text and images; Thinking:
--   Extended; the page is a legacy-model page and states no tool use or JSON schema support, so max_tools 0 and json_schema false.
INSERT INTO router_model_capabilities (provider, model, max_tools, supports_json_schema, supports_vision, supports_reasoning, effective_from) VALUES
    ('openai',    'gpt-5-mini',        NULL, true,  true, true,  '2026-11-11T00:00:00Z'),
    ('openai',    'gpt-4.1-mini',      NULL, true,  true, false, '2026-11-11T00:00:00Z'),
    ('openai',    'gpt-4o-mini',       NULL, true,  true, false, '2026-11-11T00:00:00Z'),
    ('anthropic', 'claude-sonnet-5-5', NULL, false, true, true,  '2026-11-11T00:00:00Z'),
    ('anthropic', 'claude-haiku-4-5',   0, false, true, true,  '2026-11-11T00:00:00Z');

INSERT INTO router_model_routes (provider, from_model, to_model) VALUES
    ('openai',    'gpt-5',             'gpt-5-mini'),
    ('openai',    'gpt-4.1',           'gpt-4.1-mini'),
    ('openai',    'gpt-4o',            'gpt-4o-mini'),
    ('anthropic', 'claude-opus-5-5',   'claude-sonnet-5-5'),
    ('anthropic', 'claude-opus-4-5',   'claude-haiku-4-5'),
    ('anthropic', 'claude-sonnet-4-5', 'claude-haiku-4-5');

-- +goose Down
DROP TABLE router_model_routes;
DROP TABLE router_model_capabilities;
DROP TABLE router_feedback;
