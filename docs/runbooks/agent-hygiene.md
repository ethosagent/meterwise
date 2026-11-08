# Agent hygiene (monthly)

Not repo-owned state, so it lives in a checklist. Do it on the first working day of the month.

- [ ] **Memory prune.** The memory index stays at or under 100 lines. Delete entries that are stale or duplicated here.
- [ ] **Spend review.** Run `npx ccusage` and look for sessions far above the median; read their handoffs for the cause.
- [ ] **`/context` baseline.** Open a fresh session in this repo, run `/context`, compare with the last row in
  [`../decisions/README.md`](../decisions/README.md). Target at most 30K tokens. If it grew, trim `CLAUDE.md`, skill
  descriptions or disable a connector until it is back under target, then record the new row.
- [ ] **Connectors.** Disable the ones this repo does not need (see [`local-dev.md`](local-dev.md) section "Agent setup").
- [ ] **Skills.** Fewer than 40, none unused for a month, manual-only skills carry `disable-model-invocation: true`.
- [ ] **Gate health.** `scripts/run-checks.sh` takes about as long as last month; investigate any `! SKIP` that has outlived its reason.
- [ ] **Dependabot.** Merge or close the open dependency PRs; any new package needs an allowlist entry and an ADR.
