# 0026. One body-edit protocol (`internal/wire`) and the 09-11 hook order

- **Status:** Accepted
- **Date:** 2026-11-11
- **Source:** Plans 09, 10, 11 step 0 (interface S0.1, S0.3; rulings A1-A7); extends ADR 0022

## Context

Memory (Plan 09), the router (Plan 10) and trimming (Plan 11) all edit the request body or the model
before it goes upstream. Each would otherwise carry its own wire parser and its own place in the hook
chain, and the three would disagree about order and about what the monitors may still see.

## Decision

1. **The wire package moves to `internal/wire`** (layer rank 2, pure). It is the only place that parses
   or edits a provider body; the three features and the monitors call it. `internal/monitors/wire` is gone.
2. **Hook order is fixed:** `wsauth, history, trim, memory, router, monitors, injected..., ledger`. A nil
   slot is skipped. The features see the client's body in that order, so trim shrinks first, memory adds
   a hint to the trimmed body, the router picks the model from the final body, and the monitors judge
   what was actually sent.
3. **Layers:** `trim`, `router` and `memory` are rank 3; `trim`, `router` and `wire` are pure (no I/O,
   no `store`, no `net/http`). Memory may do I/O, but never in `Before`.
4. **Fold and unfold:** a fold leaves `Meta[trim.unfold]` with the args hashes of a repeated call; the
   monitors then drop `duplicate_tool_call` and `repeated_read` verdicts for it (`unfold_skips`), because
   the repeat is the agent recovering folded content, not waste.
5. **Every feature off is byte-identical** to the pre-09-11 upstream body. A golden test through
   `HookChain` (`chain_golden_test.go`) holds this on both wires.

## Consequences

- Wave 1 and 2 owners edit disjoint files; the shared kernel is this step.
- The order is a contract: moving a feature in the chain needs a new ADR.
- Cost: trimmed text is what memory and the router see, so their quality is bounded by trim's output.
