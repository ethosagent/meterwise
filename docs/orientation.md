# Orientation: read order for a new agent

1. [`CLAUDE.md`](../CLAUDE.md): the map, the gate, the non-negotiables, the protected paths.
2. [`ARCHITECTURE.md`](../ARCHITECTURE.md): vocabulary, layers, dependency law, seams.
3. The package `doc.go` of what you will touch (`go doc ./internal/<pkg>`), and its ADRs in [`decisions/`](decisions/README.md).
4. The path rule in `.claude/rules/` that loads for your files, and the review lens for your risk tier
   (`scripts/risk-tier.sh <paths>`).
5. The plan for your task (`plan/NN-*.md`) if `plan/` exists in your checkout; it is gitignored and may be absent.
6. [`conventions.md`](conventions.md) when you write code; [`context-budget.md`](context-budget.md) when output gets large.

## First five minutes

```sh
scripts/install-hooks.sh
scripts/run-checks.sh --fast        # green on a clean tree
go run ./cmd/meterwised version -json
```

## Where things are

| Question | Answer |
|---|---|
| What can a package import? | `internal/archtest/law_test.go` and `ARCHITECTURE.md` §3 |
| Which route needs which permission? | `internal/contract/routes.go` |
| Which error codes exist? | `internal/contract/errors.go` |
| What does the schema look like? | the migrations; `docs/schema.md` is generated from them |
| How do I run something quietly? | `scripts/quiet.sh <command>` |
| How do I add X? | the skill table in `CLAUDE.md` |
