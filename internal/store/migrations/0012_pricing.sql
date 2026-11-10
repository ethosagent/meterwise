-- feature: pricing
-- +goose Up
-- Global token prices (Plan 05). Money is integer micro-USD per million tokens (ruling R12): a
-- price of $2.50 per million input tokens is 2500000. cached_per_m and reasoning_per_m are NULL
-- when the provider has no separate rate (the input or output rate applies). A price row is
-- effective from effective_from until a later row for the same (provider, model) supersedes it.
-- The seed is a starting snapshot to be reviewed against the providers' price pages.
CREATE TABLE pricing (
    provider       text NOT NULL CHECK (provider <> ''),
    model          text NOT NULL CHECK (model <> ''),
    input_per_m    bigint NOT NULL CHECK (input_per_m >= 0),
    output_per_m   bigint NOT NULL CHECK (output_per_m >= 0),
    cached_per_m   bigint CHECK (cached_per_m >= 0),
    reasoning_per_m bigint CHECK (reasoning_per_m >= 0),
    effective_from timestamptz NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, model, effective_from)
);

INSERT INTO pricing (provider, model, input_per_m, output_per_m, cached_per_m, effective_from) VALUES
    ('openai', 'gpt-4o',           2500000, 10000000, 1250000, '2024-01-01T00:00:00Z'),
    ('openai', 'gpt-4o-mini',       150000,   600000,   75000, '2024-01-01T00:00:00Z'),
    ('openai', 'gpt-4.1',          2000000,  8000000,  500000, '2024-01-01T00:00:00Z'),
    ('openai', 'gpt-4.1-mini',      400000,  1600000,  100000, '2024-01-01T00:00:00Z'),
    ('openai', 'gpt-4.1-nano',      100000,   400000,   25000, '2024-01-01T00:00:00Z'),
    ('openai', 'o3',               2000000,  8000000,  500000, '2024-01-01T00:00:00Z'),
    ('openai', 'o4-mini',          1100000,  4400000,  275000, '2024-01-01T00:00:00Z'),
    ('anthropic', 'claude-3-5-haiku',  800000,  4000000,   80000, '2024-01-01T00:00:00Z'),
    ('anthropic', 'claude-3-5-sonnet', 3000000, 15000000,  300000, '2024-01-01T00:00:00Z'),
    ('anthropic', 'claude-3-7-sonnet', 3000000, 15000000,  300000, '2024-01-01T00:00:00Z'),
    ('anthropic', 'claude-sonnet-4',   3000000, 15000000,  300000, '2024-01-01T00:00:00Z'),
    ('anthropic', 'claude-sonnet-4-5', 3000000, 15000000,  300000, '2024-01-01T00:00:00Z'),
    ('anthropic', 'claude-haiku-4-5',  1000000,  5000000,  100000, '2024-01-01T00:00:00Z'),
    ('anthropic', 'claude-opus-4-5',   5000000, 25000000,  500000, '2024-01-01T00:00:00Z'),
    ('anthropic', 'claude-opus-4-1',  15000000, 75000000, 1500000, '2024-01-01T00:00:00Z'),
    ('anthropic', 'claude-opus-4',    15000000, 75000000, 1500000, '2024-01-01T00:00:00Z'),
    ('anthropic', 'claude-3-haiku',     250000,  1250000,   30000, '2024-01-01T00:00:00Z');

-- +goose Down
DROP TABLE pricing;
