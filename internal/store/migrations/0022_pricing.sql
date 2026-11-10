-- feature: pricing
-- +goose Up
-- Plans 09-11 step 0: prices of the default route pairs that 0012 does not hold (micro-USD per million tokens,
-- ruling R12), fetched 2026-11-10 from the providers' official pricing pages (Standard tier). 0012 is never edited.
-- A model with tiered or long-context pricing the table cannot express gets no row (it stays unpriced, never mispriced).
-- source: https://developers.openai.com/api/docs/pricing (platform.openai.com/docs/pricing redirects here), fetched 2026-11-10: gpt-5 input 1.25, cached 0.125, output 10.00 per 1M
-- source: https://developers.openai.com/api/docs/pricing, fetched 2026-11-10: gpt-5-mini input 0.25, cached 0.025, output 2.00 per 1M
-- source: https://platform.claude.com/docs/en/about-claude/pricing (ids from https://platform.claude.com/docs/en/about-claude/models/overview), fetched 2026-11-10: claude-opus-5-5 input 4, cache read 0.20, output 20 per 1M
-- source: https://platform.claude.com/docs/en/about-claude/pricing, fetched 2026-11-10: claude-sonnet-5-5 input 2, cache read 0.10, output 10 per 1M
INSERT INTO pricing (provider, model, input_per_m, output_per_m, cached_per_m, effective_from) VALUES
    ('openai',    'gpt-5',             1250000, 10000000,  125000, '2026-11-10T00:00:00Z'),
    ('openai',    'gpt-5-mini',         250000,  2000000,   25000, '2026-11-10T00:00:00Z'),
    ('anthropic', 'claude-opus-5-5',   4000000, 20000000,  200000, '2026-11-10T00:00:00Z'),
    ('anthropic', 'claude-sonnet-5-5', 2000000, 10000000,  100000, '2026-11-10T00:00:00Z');

-- +goose Down
DELETE FROM pricing WHERE effective_from = '2026-11-10T00:00:00Z'
    AND (provider, model) IN (('openai', 'gpt-5'), ('openai', 'gpt-5-mini'),
                              ('anthropic', 'claude-opus-5-5'), ('anthropic', 'claude-sonnet-5-5'));
