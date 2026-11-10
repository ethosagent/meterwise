# 0024. The history ring is keyed by call index and bounded by deadline and bytes

- **Status:** Accepted
- **Date:** 2026-11-10
- **Source:** Plan 06 fix round 2 rulings B2, M4, m1, m2 (06-opus verification); extends ADR 0022

## Context

The monitors judge a call from `history`'s per-session ring (ADR 0022). `After` fills the ring once the
response is out, so a fast client sends its next request first. Fix round 1 caught the ring up by
counting assistant turns, capped at the ring depth. The verifier showed three faults: past 50 calls the
catch-up never ran; an out-of-order `After` (a blocked call finishes in microseconds) appended a
duplicate entry and fired `duplicate_tool_call` on distinct calls; and an assistant prefill was read as
a turn, firing `no_progress` on every call while missing the real repeat. It also measured a rebuild
over 1 ms with large arguments, notes lost by a late `Before`, and about 1.3 GB of rings at 100000
sessions.

## Decision

1. **The ring is keyed and ordered by call index (B2).** One entry per index, ascending, the 50 newest
   indexes. `After` puts its call at its own index, so a late or out-of-order `After` replaces that entry
   and never appends a duplicate. The ring also remembers the newest index whose `After` ran, failed
   calls included.
   - Catch-up: a request whose call index is ahead of the newest index the ring knows (the previous
     call's `After` has not run yet) fills the missing indexes from its own messages, the k-th newest
     assistant turn being index `calls - k`, at most 50 back. Only the missing turns are parsed
     (`turns_read`). It works past 50 calls. A failed call's index is known, so it is never filled
     from another turn.
   - A trailing assistant message is a prefill, not a turn: it is skipped by history and by the block.
   - A call answered without a stream (a block, or `stream:false`) leaves an empty entry; the next
     `Before` heals it from the request's newest turn. A catch-up and a heal both run only when the
     request's turns at the three indexes below match the ring's entries there (`heal_misaligned`
     otherwise, K3): a side request that shares the session id must not get the main thread's newest turn
     copied into an empty entry, nor its own turns into the index of a call still in flight. A fill never overwrites content,
     and an `After` with no content keeps what a catch-up found.
   - A session with no ring (a restart, an eviction) is rebuilt from the last 200 messages, newest 50
     turns. After a full process restart tollgate's call counter starts again, so the rebuilt turns get
     indexes below 1; evidence can then name call 0 or a negative call.
   - Not filled: a hole below the newest known index (an `After` that is late while a newer one already
     ran). The late `After` fills it.
2. **Before stays inside its deadline (m1, m2).** `history.Before` asks the chain's deadline before
   each turn it parses and stops at `MinDeadline` (5 ms) before it, keeping the newest turns it read
   (`skipped_deadline`, `cut_deadline`). `monitors.Before` checks at its start and again after
   detection, and only then takes the session's queued notes, so a late `Before` loses none. A context
   cancelled in the microseconds between that check and the write still loses that call's notes.
   Measured (201 messages, 40 trials): a full rebuild p99 0.98 ms with 2 KB arguments per turn and
   5.1 ms with 32 KB; a catch-up of one turn p99 0.04-0.32 ms.
3. **History is bounded by bytes as well as sessions (M4).** `HISTORY_MAX_BYTES` (default 256 MiB)
   is split across the shards; beyond it the least recently used sessions go (`evicted_bytes`, `bytes`
   on `/statusz`). `MaxSessions` (tollgate's limit) still caps the count. The charge per ring is an
   estimate (struct sizes, hash slices, tool names, key), not a heap count; the verifier measured about
   13 KB of heap for a full 50-entry ring.
4. **Unknown usage is explicit, and the certain estimate is a lower bound (M2, I7).** A summary read back from
   a request (catch-up or rebuild) has `CallSummary.UsageKnown == false`; only an `After` sets it. `monitors.basisOf`
   prices the block estimate from known entries only: the prior input and the body growth need a known last entry
   (else 0), and the output part is the largest value the median provably reaches given the unknown entries
   (0 when too few are known), so the estimate never exceeds the one from the real numbers
   (`TestBlockEstimateIsALowerBoundForAnyMixOfKnownAndUnknownEntries`).

## Consequences

Easier: the ring cannot hold two entries for one call, catch-up costs one parsed turn in the common
case, and memory has a hard budget. Harder: the turn-to-index mapping assumes the request carries one
assistant turn per successful earlier call; a client that rewrites old history can mislabel a caught-up
turn until its own `After` replaces it, and one that echoes an assistant turn with other text or
arguments than it streamed is never caught up (`heal_misaligned`; the next `After` fills the entry). The
alignment check costs up to three parsed turns on a catch-up (`BenchmarkWarmSession/catchup`). Enforced by `TestCatchUpWorksPastTheRingDepth`,
`TestOutOfOrderAfterKeepsOneEntryPerIndex`, `TestAssistantPrefillIsNotATurn`,
`TestAssistantPrefillOnBothWiresAndStreamModes`, `TestCatchUpPastRingDepthReadsOnlyTheMissingTurn`,
`TestFailedCallIndexIsNotFilledFromAnotherTurn`, `TestLateEmptyAfterKeepsTheCaughtUpTurn`,
`TestRingsStayWithinTheByteBudget`, `TestHistoryMaxBytes`, `TestBeforeStopsReadingAtItsDeadline`,
`TestLateBeforeLeavesQueuedNotesForTheNextCall` and
`TestSideRequestWhileTheRingLagsIsNotCaughtUpFromItsOwnTurns`.

The certain block estimate stays a lower bound of the exact one (`basisOf`): an entry counts as known only when its After reported usage (zero input is unknown), the input and body-delta parts come only from a known entry at index n-1 (a gap there makes them 0), and every index of the window `[max(1, n-RingDepth), n-1]` with no entry counts as unknown in the output floor. Enforced by `TestBlockEstimateIsALowerBoundOverGapsZeroUsageAndShuffledAfters`, `TestAbsentPreviousEntryGivesNoInputPartAndNoOutputPart`, `TestDeadlineSkippedCatchUpEstimateIsALowerBound` and `TestUnreportedUsageLeavesTheEntryUnknown` and `TestOutputFloorWindowIsTheLastRingDepthCalls`.
