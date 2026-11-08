# Data integrity (`DATA-*`)

Guards decisions D8 (one ledger writer), D11 (two savings numbers) and ruling R12 (money).

## Already covered by CI

- archtest H4 (only `ledger` and `audit` write their tables).
- `migrate` gate: file naming, one feature per file, a Down section, goose up/down/up.
- `cmd/gen migrations`: a migration touches only its own feature's tables.

## Checks

**DATA-1 Ledger writes are idempotent on `call_id`.** Replaying `Hook.After` for the
same call changes nothing: `INSERT ... ON CONFLICT (call_id) DO NOTHING` or an
equivalent, with a test that delivers the same call twice.

**DATA-2 Sessions reconcile with calls.** Every `sessions` aggregate (call count,
tokens, cost) equals the sum over its `calls`; a test asserts it after writes and
after a replay.

**DATA-3 Saved and likely saved are never summed.** No expression, DTO field, chart
or total adds the certain number to the estimated one; they stay separate columns
and separate labels.

**DATA-4 Money is integer micro-USD.** No `float32`/`float64` holds a price, cost or
saving. DB columns are `numeric(14,6)` named `*_usd`; the API sends decimal strings.

**DATA-5 Migrations are reversible and append-only.** A Down section that really
undoes Up; an applied file is never edited, a new file is added instead.

**DATA-6 Schema and docs agree.** `docs/schema.md` is regenerated, never edited; a
new column appears there in the same PR.

**DATA-7 Time is UTC and injected.** Stored timestamps are `timestamptz`; code takes
a clock, so tests never depend on wall time.

**DATA-8 No backfill in a schema migration.** Data moves in a separate, re-runnable
step with its own test.
