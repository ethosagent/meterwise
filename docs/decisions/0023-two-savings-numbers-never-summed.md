# 0023. Two savings numbers, never summed

- **Status:** Accepted
- **Date:** 2026-11-10
- **Source:** plan/06-monitors.md §4, 06-interface.md rulings R6 and R9

## Context

A monitor that blocks or notes a wasteful call saves tokens, but how sure we are differs. A block of an
exact repeat is certain: the upstream was never called and the estimate is the prior input plus the
body delta. A note that makes the agent skip a repeated read only likely saves something. An observe-mode
verdict saves nothing: it says what the monitor would have done. A single "saved" total would mix
those, overstate the product's effect and be impossible to audit. Options: one number with a
confidence band (hides which part is hard evidence); one number that counts only certain savings
(throws away the likely signal that justifies note mode); two numbers with observe kept apart.

## Decision

The ledger keeps **two** savings numbers, `saved` (confidence certain) and `likely_saved` (confidence
likely), and a third, `would_save`, for observe-mode verdicts. A verdict counts toward saved or likely
only when its action is note or block; an observe verdict is would-save and is in neither. No table,
query, DTO or screen holds the sum of saved and likely. The numbers are priced in `contract.MicroUSD`
(numeric(14,6) in Postgres, a decimal string in JSON) by the ledger, from the estimator's tokens at the
call's start time; a model with no price keeps the tokens and a zero USD value. `sessions.saved_usd`
and `likely_saved_usd` are added to in the same transaction that inserts the call and its
`monitor_events` rows, only for calls actually inserted, so a replayed `call_id` moves nothing; the unique
key `(workspace_id, call_id, monitor)` is the backstop. A block verdict on a call that already has usage
(the upstream was reached first) is downgraded to observe and counted in `blocks_downgraded`.

## Consequences

Easier: every figure on a dashboard traces to event rows (`SELECT SUM(est_saved_usd) ... WHERE
action <> 'observe' AND confidence = 'certain'`), and the false-positive rate (wrong over correct plus
wrong feedback) is computed per monitor beside them. Harder: the UI shows two figures and has to
explain them; "total saved" is a product decision that needs a new ADR, not a quiet addition. A
change to the estimator moves future events only: stored rows are never re-priced. Enforced by
`TestRealPostgresVerdictsReconcileWithSessionSums` and `TestRealPostgresReplayedCallWritesNoSecondEvent`
(internal/ledger), `TestWriteCallsWritesMonitorEventsAndSessionSums` (internal/store),
`TestMonitorStatsRoute` (internal/api, which also asserts the wire form never carries a sum), and
`TestLateBlockIsDowngradedToObserve`.
