# 0022. Monitor verdicts cross the chain through a Meta pointer; config via a tenant attr

- **Status:** Accepted
- **Date:** 2026-11-10
- **Source:** plan/06-monitors.md, 06-interface.md rulings R1 to R10, the fix round 2 rulings (B1-B3, M1-M4, m1-m3)

## Context

Plan 06 adds three hooks in a fixed order, `Deps.Hooks = [history, monitors, ledger]` (R2). `history`
keeps a per-session ring, `monitors` decides, `ledger` writes the rows. Four facts of tollgate v0.1.0
shape how they talk:

1. A `Before` that returns a `*Response` ends the chain, but that hook's own `Call` copy is not
   adopted, so its `Meta` writes are lost (gap 1). The ledger still needs the verdict of a blocked call.
2. A `Before` hook works on a private deep copy. Map and slice `Meta` values are cloned; pointers are shared.
3. `OnEvent` runs on a tap off the client path and may drop frames under load. `After` runs after the tap settles.
4. The pure packages may not import each other (archtest H3): `monitors` may not import `history` or `savings` (R1).

Options for carrying a verdict to the ledger: a shared registry keyed by call id (global state, needs
eviction, two hooks must agree on keys); a second write path from `monitors` to the store (breaks one
writer per table, H4); return the verdict in the `Response` (only works for blocks).

## Decision

1. **A pointer in `Meta`.** `history.Before` never short-circuits, so its copy is adopted. It puts a
   `*contract.CallVerdicts` at `Meta["monitors"]` and a read-only `contract.RingView` at
   `Meta["history.ring"]`. `monitors` writes verdicts through the pointer, as the last statement of
   `Before` and only while `ctx.Err() == nil`; with no pointer it does nothing. The ledger seals the
   collection in `After`, which also makes a late write a no-op. `CallVerdicts` keeps one verdict per monitor.
2. **Config is a tenant attr.** `wsauth` encodes `settings.monitors` once per resolve into
   `Tenant.Attrs["monitors"]` (canonical JSON of the typed `contract.MonitorConfig`; the tenant is
   cached). `monitors` decodes it through a memo (string to config, cap 1024), so `Before` does no
   I/O and no per-call parse. A PATCH busts the directory cache, so the next call uses the new mode.
   `Policy.Overrides` is not used: a later tollgate may apply it to the request body.
3. **One hash alphabet in `contract`.** The ring holds hashes, tool names, counts and token numbers,
   never text (R7). Both pure packages must agree on how a turn becomes hashes and may not import each
   other, so `contract/monitors_hash.go` owns it: `HashString`, `ToolArgsHash` (top-level
   order-insensitive, whitespace-free, allocation-free), `ToolSubject`, `ClassifyTool`, `TextHasher`,
   `TurnShingles` and `HashID` (sha256 of workspace id and value, 16 hex). Evidence carries `HashID`s.
4. **Provider JSON is edited in one place**, `monitors/wire` (R4). `AppendNotes` splices the call's notes
   by offset in one scan and copy (the bytes outside the splice are identical); OpenAI gets a trailing system message,
   Anthropic an extra text block at the end of the last user message, never `system` (editing it
   would invalidate the prompt-cache prefix on every noted call). `BlockResponse` builds the whole
   JSON or SSE answer: one terminal assistant turn that holds only text (`finish_reason: stop`,
   `stop_reason: end_turn`), no tool call. Nothing is replayed, from memory or from the request.
5. **Blocking ends the agent's turn (B1).** A replayed turn named a tool; the agent ran it, the next
   request matched again, and the agent looped with the model never called. So a block answers with a
   `[proxy]` text that names the monitor's facts and where the earlier identical result is (the id of the
   tool call that received it, a value the client already holds). A block needs that earlier identical
   result in the request, else it degrades to a note (`blocks_degraded`). At most one block per session
   and subject (argument or path hash): a later repeat of that subject is noted and goes upstream
   (`blocks_capped`). No tool call id is ever reused. A subject's block or note is used up only when it
   is delivered (S2, K1, K2): the session store reserves its key before the edit, so two concurrent
   `Before`s of a session deliver it once, and `Before` commits the reservation after its last context
   check. A failed edit or a chain that stopped waiting releases it. Queued stream notes follow the same
   rule: a note leaves the queue and uses its subject up only when spliced in; a failed splice puts it
   back (`notes_dropped`), and a stream subject is reserved from the moment its note is queued.
6. **Detection rules the plan left open.**
   - `duplicate_tool_call` counts the pending call itself: it fires at `maxRepeats` total occurrences
     within `lookBack` earlier calls, with no write since the earliest. A write that the pending turn
     makes before the call breaks it too (M2); the same holds for `repeated_read`. Fuzzy matching
     compares token sets of the request's own arguments (the ring has hashes only).
   - Stream monitors collect text in `OnEvent` and decide in `After`, so they add nothing to the
     client's first byte (R8). In note mode the note waits for the session's next call (at most
     `MaxNotesPerCall` = 2 per call, deduped per session on monitor and subject). `reasoning_loop`
     counts reasoning text only (an answer may show code before and after a fix on purpose).
     `second_guessing` counts phrases of two words or more only (`wait`, `actually`, `hmm` are everyday
     wording) and needs a reversal to the very call of two calls ago: same tool and same arguments
     hash, A-B-A (M3).
7. **The history ring, its catch-up and its bounds** are recorded in ADR 0024 (fix round 2: B2, M4,
   m1, m2).
8. **Estimates are tokens, one per call (M1).** `savings.Estimate` returns `contract.Usage` and a
    confidence; the ledger prices it (R6). Every verdict of a call describes the same avoided call, so
    the ledger keeps one estimate per call: the largest certain one that acted (a block), else the
    largest likely one that acted (a note), else the largest would-save of an observed one. The other
    verdicts record 0 and keep their evidence. Observe mode is would-save, never in either number. The
    stats API reports tokens and USD per class (`savedTokens`, `likelySavedTokens`, `wouldSaveTokens`
    and the three USD fields): no field adds two classes (B3).
9. **Feedback uses `session:read` (m3).** Marking an event correct or wrong is how a reader of a
    session reviews it; it changes only the event's `feedback` column and the false-positive rate. It
    is audited (`monitor_event.feedback`), scoped to the workspace in the path (an event of another
    workspace is a 404), and a member without that workspace's grant cannot reach it. A separate
    permission would gate review behind admin rights for no data gain; revisit if feedback ever drives
    an automatic mode change.

## Consequences

Easier: the ledger sees every verdict, blocked or not, with no new store path and no global state;
config changes need no restart; the two pure packages stay independent. Harder: the pointer is
shared mutable state across a copy boundary, so it is only written before the chain's deadline and
sealed by its one reader; a hook order other than the fixed one silently disables monitoring (no
pointer, no verdict). Stream detection is best-effort (frames may drop). A `stream:false` response
is not read by the detectors. The ring's shingles are a 64-window prefix of three turns, so a
cross-turn loop is judged on how responses open. The hash of an argument object ignores top-level
member order but not nested order.

Enforced by archtest H3 (purity) and H8, `TestDetectorsOnRingFixtures` (a positive and a negative
fixture per detector, exact evidence), `TestRecreatedHistoryGivesTheSameVerdictsAndBlock`,
`TestBlockAnswersWithoutCallingTheUpstream`, `TestLoopingAgentFinishesUnderBlockOnBothWiresAndStreamModes`,
`TestSecondRepeatOfABlockedSubjectIsNotedNotBlocked`, `TestPendingTurnsOwnWriteBeforeAReadBreaksThePattern`, `TestOneCallIsAttributedOnce`,
`TestMonitorStatsTokenFieldsNeverSumCertainAndLikely`,
`TestNoteIsExactlyOneSplicedNoteAndNotRepeated`, `TestObserveLeavesEveryByteAlone`,
`TestBeforeP99UnderOneMillisecondFor200Messages`, `TestStreamDetectorsAddNothingToTTFB`,
`TestRingHoldsNoMessageText` and `TestPrivacyNothingFromTheConversationReachesVerdicts`.
