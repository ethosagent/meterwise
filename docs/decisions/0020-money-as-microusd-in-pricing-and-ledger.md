# 0020. Money is micro-USD in pricing and the ledger

- **Status:** Accepted
- **Date:** 2026-11-10
- **Source:** Plan 05 section 6 (overridden by ruling R12); non-negotiable 5

## Context

Plan 05 sketched `Price(...) (usd float64, ok)`. Floats cannot add up money exactly and the two savings
numbers must reconcile with sums of rows. Options: float64 (rejected), `numeric` end to end in Go
(needs a decimal dependency, H6), integer micro-dollars (chosen).

## Decision

Go carries money as `contract.MicroUSD` (int64 millionths of a dollar). `pricing` stores bigint
micro-USD per million tokens; `savings.TablePricer.Cost(provider, model, Usage, at)` multiplies tokens
by rates in int64 (overflow returns "unknown"), sums, and divides once, rounding half up. An unknown
model or a time before the first price row is `ok=false` and the call's `cost_usd` is NULL (flagged,
counted in Status, logged once per model), never 0. Ledger columns keep the `*_usd` names as
`numeric(14,6)`, which holds a micro-dollar exactly; queries convert at the edge. The API sends money as
a decimal string (`"1.500000"`), never a JSON number (the interface note "serialises as an integer" is
overridden by R12). A model name matches a price row exactly or as row name plus a date, `-latest`, or
a `:`/`@` qualifier, so `claude-opus-4-5` never inherits the `claude-opus-4` price.

## Consequences

Easier: sums are exact; a session cost equals the sum of its calls.
Harder: the seed prices are a starting snapshot to be reviewed against the providers' price pages;
tokens the neutral model does not carry (Anthropic cache writes) are not priced.
Enforced by: `TestCost`, `TestCostOverflowIsUnknownNotWrapped`, `TestUnknownModelIsNullCostAndCounted`,
contract money round-trip tests.
