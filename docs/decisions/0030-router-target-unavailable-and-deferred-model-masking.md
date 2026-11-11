# 0030. Router: a refused target pauses the pair; model masking and the route header are deferred

- **Status:** Accepted
- **Date:** 2026-11-11
- **Source:** Plan 10 rulings R9, A1 and A3 (wave 1 controller); extends ADR 0029

## Context

A routed call can be refused by the provider: the key has no access to the cheaper model, or the model is retired.
The plan asked for a replay on the strong model. tollgate's `Finalizer` sees only 2xx streams, and a hook cannot
issue a second upstream request, so a replay on 400/403/404 cannot be done from a hook. Plan 10 also listed
`maskModel` (rewrite the response `model` field back to the requested model) and an `x-meterwise-route` header.

## Decision

1. **No replay (A1).** When a routed call returns 400, 403 or 404, the router's `After` marks that pair (workspace,
   provider, from, to) unavailable for one hour. Calls for that pair are then held with reason `unavailable` and go
   out on the model the client asked for. The marks are in memory, bounded (4096 entries) and dropped on expiry.
2. **The failed call is still the client's problem, once.** The client sees the provider's error for that one call.
   The ledger records it as a routed call with zero saving, so the stats count it in `unavailable`; the Settings
   card says how many were refused. `/statusz` carries `unavailable_marked`, `unavailable_dropped` and
   `unavailable_now`.
3. **A 400 also pauses the pair.** A 400 can be a bad request unrelated to routing; pausing for an hour is the
   cautious choice, since the next call would otherwise repeat the cost.
4. **`maskModel` is deferred (R9).** The response keeps the model that ran. The ledger and Sessions show both the
   requested and the served model.
5. **`x-meterwise-route` is deferred.** There is no per-request opt-out header yet; the workspace mode is the switch.
6. **Held reasons `score` and `edit_failed` are router-local.** They are not in `contract` (the shared reasons are
   `first_steps`, `escalated`, `error_retry`, `unavailable`, `not_in_map`, `unpriced`, `capability`).
7. **Waiver: `router_feedback` has no `org_id`.** The table is scoped by `workspace_id` alone. A deployment has one
   organization, so a workspace id already identifies the tenant. If multi-org arrives, an additive migration adds
   the column; no schema change now.

## Consequences

- One failed call per pair per hour at worst, instead of silent repeated failures.
- A restart forgets the marks; the next routed call finds out again.
- Replay can return when tollgate gives hooks a retry (see the Plan 10 deferred list).
- Enforced by: `TestRefusedTargetMarksThePairUnavailableThroughTheChain` (dataplane),
  `TestARefusedRoutedCallRecordsItsRouteAndNothingElse` (ledger) and
  `TestARefusedTargetIsCountedAndTheNextCallStaysOnTheStrongModel` (cmd e2e).
