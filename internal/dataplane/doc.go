// Package dataplane builds the embedded tollgate proxy (decision D1). New wires
// Deps into tollgate.Options; Plans 04-06 replace fields of Deps (workspace
// authenticator and resolver, admission, hooks, ledger sink) and never rewire
// this package. HookChain is the one place the hook order is written (wsauth,
// history, monitors, injected, ledger). It may import config, contract, wsauth,
// history and monitors (never the ledger package: the ledger arrives as a
// tollgate.Hook) and tollgate's public surface.
package dataplane
