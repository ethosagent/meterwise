# Hot path (`HOT-*`)

The host's own work per call must stay far below a millisecond; upstreams take seconds.
Applies to `dataplane`, `history`, `monitors`, `savings` and the ledger enqueue.

## Already covered by CI

- Lint bans `http.DefaultClient` and friends (no timeouts) and requires a context on outbound requests (`noctx`).
- archtest H3 keeps the pure packages free of I/O imports.

## Checks

**HOT-1 No unbounded blocking call per request.** Any network or database call on the
request path has a deadline and a defined fall-back (skip the check, pass through).

**HOT-2 Once, not per request.** Regex compilation, price-table parsing, settings decoding
and client construction happen at start-up or on a settings change, not per call.

**HOT-3 Stream, do not buffer.** Never read a streaming body in full.

**HOT-4 Parse once.** Hooks share the `Call`; no second `json.Unmarshal` of the same body.

**HOT-5 Allocation awareness.** Per-call code reuses buffers where it can; benchmarks
cover the history ring append and each monitor check.

**HOT-6 No process-wide locks per call.** Shard by workspace or session; a global mutex on
the call path is a finding.

**HOT-7 The ring is bounded.** The per-session history ring has a hard size and an eviction
rule, with a test at the boundary and with many sessions.

**HOT-8 Benchmarks with the change.** A change to per-call code adds or updates a
`Benchmark*` and states the before and after in the PR.
