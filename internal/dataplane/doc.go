// Package dataplane builds the embedded tollgate proxy (decision D1). New wires
// Deps into tollgate.Options; Plans 04-06 replace fields of Deps (workspace
// authenticator and resolver, admission, hooks, ledger sink) and never rewire
// this package. It may import config, contract and tollgate's public surface.
package dataplane
