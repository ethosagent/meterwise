// Package router is the cheaper-model router (Plan 10): a pure tollgate Hook that, for a workspace that opted in,
// replaces one field of a request, the top-level model, with a cheaper sibling from the same provider. The call is
// still the customer's own, on the customer's key and bill (ADR 0029); the router holds no key and makes no model
// call. The score (score.go) decides how simple the step is from the history ring and the request; the guards
// (guard.go) decide whether the target can serve the request; monitors are the safety net: a verdict on a routed
// call forces the next calls back to the strong model.
//
// Before does no I/O: the workspace's settings, the route map, the capability table and the priced-model set are
// read from memory (Tenant.Attrs["router"] through a memo, contract.RouterTables built at boot by BuildTables).
// The body is edited only through internal/wire. The package is pure (archtest H3): stdlib, tollgate's root,
// internal/contract and internal/wire.
package router
