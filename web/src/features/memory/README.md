# memory (web)

Memory on the web, in the order a customer meets it. `MemoryHintEvent` is the Remembered card on a session step:
the exact hint, the similarity, the source session, the likely saving (grey, never green; "would have hinted" in
observe) and 👍 helped / 👎 irrelevant. `MemoryRow` is the Memory row of the Savers card: the mode (Hint waits behind
a confirm), similarity threshold, hints per session, the two model names, memory's own cost this month and Forget
memory (workspace:manage, behind a confirm). `TracesPage` (Settings, `/settings/memory`, session:read) lists the
remembered solutions with Load more; `TraceDrawer` shows one in full with "Reused N times" and the sessions it helped.
`memoryText.ts` holds the disabled and empty-state sentences (no key, privacy, budget, nothing learned yet,
observing); `useFirstHint.ts` is the one-time first-hint toast. The Overview card is `overview/MemoryCard.tsx`.
Queries are in `api/memory.ts`; fixtures in `test/fixtures/memory.ts`, routes in `test/mockMemory.ts`.
