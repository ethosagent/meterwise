# 0029. The router changes one field, on the customer's key; monitors are the safety net

- **Status:** Accepted
- **Date:** 2026-11-11
- **Source:** Plan 10 (cheaper-model routing); builds on ADR 0026 (one body-edit protocol) and ADR 0027 (savings causes)

## Context

Routing sends an easy step to a cheaper model of the same provider. Two things could make that unsafe: editing more
of the request than the model name, and routing a call to a model that cannot serve it. A third is policy: whose
key and whose bill the routed call uses.

## Decision

1. **One field.** In `conservative` and `aggressive` mode the router replaces the top-level `"model"` string of the
   request body (a byte splice, `wire.SetModel`) and sets `c.Model` to match, so the ledger prices the call at the
   model that ran it. Nothing else in the body changes. `observe` sends the client's bytes untouched and only records
   what it would have done. Provider JSON is never parsed in the hook beyond that field and the capability flags.
2. **The customer's key and provider.** The `Authorization` / `x-api-key` header passes through as for every call.
   A pair never crosses providers; the cheaper model is billed to the customer's own account.
3. **Monitors are the safety net.** The router does not judge answers. A step routes only when its score is at or
   under the threshold and none of the hold rules apply: the first two calls of a session, an escalation (a monitor
   fired on a routed call: the next two calls stay strong), a failing tool result, or a capability mismatch. The
   guardrail compares monitor fire rates of routed and other calls and warns when the routed rate is over 200 bp
   higher; a person can also mark a routed call "worse".
4. **Capability guard.** A call is never routed to a model that cannot take what the request carries: tools (counted
   across `tools` and `functions`), vision input, JSON-schema output, reasoning controls. A model whose `max_tools`
   is 0 takes no tools. When `max_tools` is NULL (the provider page states tool support but no maximum) the router
   applies the named constant `router.DefaultMaxTools` (32). The constant lives in Go, not in seed data, and is
   tested at its boundary.
5. **Priced pairs only.** A default pair is kept only when both ends are priced and the target has a capability row.
   Pairs dropped at boot are logged with a reason. Prices and tables are read once, at start.

## Consequences

- A bad route costs one answer, visible as a monitor event or a "worse" mark, and the next two calls stay strong.
- The cap of 32 is a guess until a provider states a real limit: raise it by editing the constant and this ADR.
- Tables load at boot: a price change reaches the router at the next restart.
- Enforced by: `internal/router` tests (score table, guard, `DefaultMaxTools` boundary), `internal/wire`
  `TestSetModel*`, `internal/dataplane` `TestConservativeChangesOnlyTheModelOnBothWires`,
  `TestCapabilityGuardHoldsOnBothWires`, `TestEscalationThroughTheChain`, and the archtest rule H3 (the router
  imports no I/O).
