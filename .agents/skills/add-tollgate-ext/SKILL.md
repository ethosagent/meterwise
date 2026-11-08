---
name: add-tollgate-ext
description: Wire a tollgate extension (authenticator, resolver, admission, hook) into dataplane.Deps with a testkit test. Use when the data plane package exists.
---

# Add a tollgate extension

Rules first:
- Use tollgate's **public** API only (archtest H1): the root, `ext/**`, and `testkit` in tests.
- Extensions on the call path declare `InlineSafe` only if they do no I/O; otherwise they run under tollgate's deadline.
- Wiring is code in `internal/dataplane` (`Deps` to `tollgate.Options`); no globals, no `init()`.
- The test drives the real proxy against `testkit.FakeUpstream`: no network, no provider keys (R6, R7).
- Credentials pass through; nothing here may log or store `Authorization` or `x-api-key` (PRIV-2).

## Steps

1. `.agents/skills/add-tollgate-ext/scripts/new-ext.sh <name>` creates `internal/dataplane/ext_<name>_test.go`
   with the failing testkit test (needs the dataplane package).
2. Implement the extension in the package that owns the concern (`wsauth`, `history`, `monitors`, `ledger`).
3. Add it to `dataplane.Deps` and the options builder; document the field in `ARCHITECTURE.md` §6 if it moves a seam.
4. Review with `streaming`, `hot-path`, `tenancy-authz` (critical tier).
