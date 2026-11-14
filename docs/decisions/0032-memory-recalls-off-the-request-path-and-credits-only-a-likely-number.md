# 0032. Memory recalls off the request path and credits only a likely number

- **Status:** Accepted
- **Date:** 2026-11-12
- **Source:** Plan 09, rulings R1, A4, R14; builds on ADR 0023 (two savings numbers) and ADR 0027

## Context

Memory distils a finished session into a trace and, when a later session looks similar, appends one hint line to its
request. Recall needs an embedding call and a database query. Non-negotiable 1 says no hook stalls the stream and
there is no I/O in `Before`. Non-negotiable 3 says privacy mode is on by default and only metadata is stored unless
a workspace opts in. Non-negotiable 5 says a saved number and a likely-saved number are never summed.

## Decision

1. **Recall is off the path (R1).** After call 1 of a session (or at an error-retry moment, calls 2 and 3), `After`
   queues a recall job. A per-workspace worker embeds the first user message (at most 200 characters) and the error
   lines, asks pgvector for the 3 nearest traces, keeps the best one at or over the workspace threshold that the
   session has not been given, and caches its rendered hint on the session. `Before` only takes that cached hint
   under a short lock, so hints land from call 2, never later than call 3. Measured: `Before` p99 0.21 ms with a hint
   spliced into a 97 KB body, 1 us when nothing is cached.
2. **One line, bounded.** In `hint` mode `Before` appends one `[proxy] memory:` note (at most 400 bytes) with
   `wire.AppendNotes` (ADR 0026). On any wire error the body is left as it came and `edit_failed` is counted.
   `observe` records the hit and injects nothing. A session takes at most `maxHintsPerSession` hints (1-3, default 1).
3. **The hint is never stored.** `memory_hits` holds ids, similarity and mode. The session detail re-renders the hint
   from the trace, so what the page shows is what a deterministic render of the trace gives.
4. **Capture follows the ledger's text rule (A4).** Memory records, distils and recalls only for a call the ledger
   would capture text for: workspace capture on, global privacy off, workspace privacy off. Otherwise the settings
   say `privacy` and nothing is kept, not even the per-session summary.
5. **What is captured.** A completed session with at least 4 calls and at least one write tool is distilled into a
   trace. Any other closed session leaves a failure record (signature and last error), no model call. The summary the
   model sees is the ring-summary kind of fact (first user message, tool names, files, error lines, last assistant
   text), run through the redaction pass of ADR 0033.
6. **Likely credit only.** When a session that received a hint completes, the ledger credits its first injected hit
   with `(median calls of the workspace's completed un-hinted sessions over 30 days - this session's calls)` average
   calls, priced at that baseline's average call, capped at one average session, and only with at least 3 baseline
   sessions. The credit goes to `memory_hits.est_saved_*` and `sessions.likely_saved_usd`. It never touches `saved_usd`.
7. **Feedback.** Hit feedback is written by the ledger (H4: it is the one writer of `memory_hits`). Three downvotes
   and no upvote lower a trace's confidence by 0.2, three upvotes raise it by 0.2 (a generated column), so a demoted
   trace drops under the recall floor of 0.6 without a job.
8. **State is bounded.** At most 50 000 session summaries; an idle one is dropped after an hour; the ledger's close
   callback takes it. Jobs: 64 queued per workspace, 1 024 overall, 2 running per workspace; a job over a limit is
   dropped and counted, never waited for.

## Consequences

- The first call of a session never gets a hint. That is the price of no I/O in `Before`.
- The credit is an estimate against the workspace's own history; with fewer than 3 baseline sessions it is zero.
- "Same repo first" ranking is not built: no client sends a repo identity yet. The signature field exists for it.
- Enforced by: `internal/memory` tests (`TestHintLandsOnCallTwoAsOneLine`, `TestPrivacyAndOffRunNothing`,
  `TestLikelyCreditIsOnceAndBoundedByTheBaseline`, `TestBeforeP99UnderOneMillisecond`), ledger `OnClosed` and
  `memory_hits` tests, and `TestMemoryNeverAddsToTheCertainNumber`.
