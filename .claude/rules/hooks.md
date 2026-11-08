---
paths:
  - "internal/history/**"
  - "internal/monitors/**"
  - "internal/ledger/**"
  - "internal/dataplane/**"
  - "internal/savings/**"
---

# Hook and data-plane rules

You are on the call path. Checklists: `.agents/reviews/streaming.md` (STR-*), `hot-path.md` (HOT-*), `data-integrity.md` (DATA-*).

- `history`, `monitors`, `savings` are **pure**: stdlib, tollgate root, `internal/contract`. No I/O, no `net`, no `database/sql` (archtest H3).
- No I/O in `Before`; read only in-memory state. Slow work goes to a bounded queue (STR-2, STR-6).
- Every ring, channel and queue is bounded; overflow drops and counts (STR-3, HOT-7).
- Hooks share the neutral `Call`; never parse provider JSON or buffer a body (STR-5, HOT-4).
- Only `ledger` and `audit` write the ledger tables, idempotent on `call_id` (H4, DATA-1).
- Saved (certain) and likely saved (estimated) are never summed; money is `contract.MicroUSD` (DATA-3, DATA-4).
- Log ids and metadata only: no bodies, no credentials, never the public workspace id (PRIV-1, PRIV-2, PRIV-6).
- Tests use the mock twins and `testkit.FakeUpstream`; fixtures are synthetic.
- Add a `Benchmark*` for changed per-call code (HOT-8).
