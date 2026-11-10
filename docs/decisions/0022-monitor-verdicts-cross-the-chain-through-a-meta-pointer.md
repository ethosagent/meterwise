# 0022. Monitor verdicts cross the chain through a Meta pointer; config via a tenant attr

- **Status:** Accepted
- **Date:** 2026-11-10
- **Source:** plan/06-monitors.md, 06-interface.md rulings R1 to R8

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
4. **Provider JSON is edited in one place**, `monitors/wire` (R4). `AppendNote` splices bytes by
   offset (the bytes outside the splice are identical); OpenAI gets a trailing system message,
   Anthropic an extra text block at the end of the last user message, never `system` (editing it
   would invalidate the prompt-cache prefix on every noted call). `BlockResponse` builds the whole
   JSON or SSE answer from the request's own messages; nothing is replayed from memory.
5. **Detection rules the plan left open.**
   - `duplicate_tool_call` counts the pending call itself: it fires at `maxRepeats` total
     occurrences within `lookBack` earlier calls, with no write since the earliest. Fuzzy matching
     compares token sets of the request's own arguments (the ring has hashes only).
   - A block replays the assistant turn that followed the earlier identical tool result, and only
     when that turn is not the pending turn itself (replaying it would make the client repeat the
     call forever). Otherwise the block degrades to a note and is counted (`blocks_degraded`).
   - Stream monitors collect text in `OnEvent` and decide in `After`, so they add nothing to the
     client's first byte (R8). In note mode the note waits for the session's next call (at most
     `MaxNotesPerCall` = 2 per call, deduped per session on monitor and subject).
   - `history` fills a ring entry from the stream. A call answered without one (a block, or
     `stream:false`) leaves an empty entry that the next `Before` heals from the client's last
     assistant message; an empty ring is rebuilt from the last 200 messages (restart safe).
6. **Estimates are tokens.** `savings.Estimate` returns `contract.Usage` and a confidence; the ledger
   prices it (R6). Observe mode gets the same estimate and is would-save, never in either number.

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
`TestBlockAnswersWithoutCallingTheUpstream`, `TestNoteIsExactlyOneSplicedNoteAndNotRepeated`,
`TestObserveLeavesEveryByteAlone`, `TestBeforeP99UnderOneMillisecondFor200Messages`,
`TestStreamDetectorsAddNothingToTTFB`, `TestRingHoldsNoMessageText` and
`TestPrivacyNothingFromTheConversationReachesVerdicts`.
