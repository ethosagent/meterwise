// Package archtest enforces ARCHITECTURE.md's dependency law against every
// package, source file and web import in the repository: D1 (every package is
// in a layer), P1 (doc comment), H0 (contract imports the standard library
// only), H1 (tollgate's public API only), H2 (layering), H3 (hot-path
// packages are pure), H4 (one writer per ledger table), H5 (web imports) and
// H6 (third-party allowlist). It has no production code; the rules live in
// law_test.go and run as gate "arch" of scripts/run-checks.sh.
//
// Every rule has a seeded violation in seeded_test.go that must go red; a rule
// without one fails TestEveryRuleHasASeededViolation.
//
// This package is a protected path (.agents/risk.yaml, tier gates): an agent
// must not loosen a rule or extend an allowlist without an ADR and a
// maintainer's approval.
package archtest
