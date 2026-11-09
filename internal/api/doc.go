// Package api is the control-plane HTTP surface (decision D1): the route table
// of internal/contract bound to handlers, the middleware chain (request id,
// logging, recover, OTel, principal), typed JSON errors and the static web app.
// Every route must have a row in contract.Routes; New refuses to build a mux
// where the two disagree. A principal is attached in exactly one place, the
// PrincipalResolver middleware; handlers read it with PrincipalFrom. It may
// import config, contract, store and the OpenTelemetry API.
package api
