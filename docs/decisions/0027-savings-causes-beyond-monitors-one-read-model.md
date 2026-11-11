# 0027. Savings causes beyond monitors: one read model, trim and router in the headline, memory likely

- **Status:** Accepted
- **Date:** 2026-11-11
- **Source:** Plans 09, 10, 11 step 0 (interface S0.7); extends ADR 0023 and ADR 0025

## Context

Plan 07 reads savings from `monitor_events` and shows six causes. Trimming, routing and memory save
tokens or money on calls that no monitor fired on, so they cannot live in that table.

## Decision

1. **Nine cause rows**, one `CauseCard` shape: the six monitors, `trim`, `router`, `memory`.
2. **Trim and router read from `calls`** (`saved_usd_trim`, `saved_usd_router`, the action columns).
   Observe-mode amounts sit in the same columns, told apart by the action column, and feed only
   would-save. Memory is a zero row until its hits exist.
3. **Confidence per cause (Decision):** trim and router rows carry `certain`, because their amounts are
   price deltas on measured usage. Trim falls back to `likely` per call when the input was not measured
   (the call's own `trim_confidence`). Memory is always `likely`.
4. **Headline:** `savedUsd` = monitor certain + trim certain + router. `likelySavedUsd` carries every
   likely amount, memory's included. The two are never summed, in a field or in the UI (ADR 0023).
   Per-day, per-client and per-session figures reconcile with `SUM(sessions.saved_usd)`.
5. **Overview stops running `SavingsCauseTops`** (R17); one query, `SavingsCallSavers`, groups the call
   savers by UTC day with a totals row. Biggest saves (`Saves`) stays monitor-only.
6. **Sessions** carry `trimTokens`, `routedCalls` and `hints`, and the list filters by `tag`
   (`trimmed|routed|remembered`).

7. **Settings patches (Decision):** `RouterConfigPatch` carries `ResetMap` (true returns the workspace to the
   default map, since a PATCH cannot otherwise set `map` back to null) and `Touched()` (the fields a patch names,
   for audit). Both go beyond the interface table and are in the generated twin.
8. **Router capabilities:** the 0021 seed holds only what each provider page states. `max_tools` is NULL when a
   page supports tool use and states no maximum, 0 when no tool may be sent. The tool-count cap is a constant in
   the router package (wave 1), not seed data.

## Consequences

- The 07 bench is re-run with the new branch: p95 stays under 300 ms at 50k calls (overview 86 ms,
  causes 179 ms, without `-race`).
- A new saver adds a row to `SavingsCauses` and a branch to `SavingsCallSavers`, nothing else.
- Memory can never raise the headline; it can only grow the likely figure.
