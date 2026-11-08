# Review rubrics

One file per **lens**, so a reviewer looks at one kind of risk at a time. Each
check has a stable ID. Rubrics hold checks that need **judgement**; anything a
script can decide belongs in `scripts/check-*.sh`, `.golangci.yml` or
`internal/archtest` instead.

| Lens | File | IDs | Use it when |
|---|---|---|---|
| Streaming | [`streaming.md`](streaming.md) | `STR-*` | Hooks and anything on the proxy path |
| Privacy | [`privacy.md`](privacy.md) | `PRIV-*` | Logging, metrics, capture, headers, errors, tables |
| Hot path | [`hot-path.md`](hot-path.md) | `HOT-*` | Code that runs per call or per chunk |
| Test quality | [`test-quality.md`](test-quality.md) | `TEST-*` | Every PR that adds or changes behaviour |
| Tenancy and authz | [`tenancy-authz.md`](tenancy-authz.md) | `TNT-*` | Queries, handlers, routes, workspace ids |
| Data integrity | [`data-integrity.md`](data-integrity.md) | `DATA-*` | Ledger, sessions, savings, money, migrations |

Which lenses a PR needs comes from its risk tier (`.agents/risk.yaml`,
`.agents/pipeline.yaml`).

## Finding format

```json
{
  "lens": "hot-path",
  "rule": "HOT-6",
  "severity": "critical",
  "file": "internal/history/ring.go",
  "line": 42,
  "issue": "Record takes a process-wide mutex on every call",
  "evidence": "ring.go:42 locks globalMu before appending",
  "suggestion": "shard the ring by session id so calls contend only within a session"
}
```

`evidence` points at code, a test or command output. No evidence = a question,
not a finding. Use `STR-0`, `PRIV-0`, … for a real problem the rubric lacks.

## Severity

| Severity | Meaning | Action |
|---|---|---|
| critical | Breaks a non-negotiable: client stalled, credential logged, cross-workspace data visible, a hook can take the proxy down | Block |
| high | Data loss, wrong metric or money, broken acceptance criterion | Fix before merge |
| medium | Cost later: coupling, missing test for a risky branch | Fix or record why not |
| low | Naming, small cleanups | Optional |

## From review to rule

When the same rule ID is confirmed on two PRs, turn it into a deterministic
check (archtest rule, lint rule, test) and mark it **covered by CI** here.
