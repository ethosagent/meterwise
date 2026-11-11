# 0028. Trimming: a Go port of the lossless stages, sticky per-region decisions, byte-based estimate

- **Status:** Accepted
- **Date:** 2026-11-11
- **Source:** Plan 11 (`plan/11-trimming.md`); controller rulings R6-R8 and A5; extends ADR 0026 and ADR 0027

## Context

Tool results (build logs, diffs, repeated file reads, JSON listings) are the bulk of an agent's input tokens. Plan 11
shrinks them before the provider sees them. Three forces shape it:

- **Prompt caching.** A provider caches the request prefix. A rewrite that changes an earlier byte on one call and not
  the next makes every later call a cache miss, which costs more than the trimming saves.
- **The hot path.** Trimming runs in `Before`: no I/O, a 50 ms hook deadline, and a budget of 1 ms per 100 KB.
- **Where the algorithms come from.** `llmtrim-core` implements the stages in Rust. Options: link it with cgo, run it
  as a second proxy in front of the provider, or port the lossless stages.

## Decision

1. **A Go port of the lossless stages** in `internal/trim` (pure: stdlib, tollgate root, `contract`, `wire`; H3):
   `repeat_collapse`, `dup_lines`, `log_window`, `json_table`, plus `schema_trim` (aggressive mode only). No
   third-party dependency (H6). `llmtrim-core` through cgo stays an option for a later step if a stage needs it;
   it would add a build constraint (H7) and so needs its own ADR.
2. **No double proxy.** A second hop in front of the provider doubles latency, hides the request from the ledger and
   needs the provider key twice. Trim is a hook in the one chain, ahead of `memory`, `router` and `monitors`.
3. **Protect the system prompt and tool definitions only (A5).** Decisions are per tool-result region, keyed by the
   hash of the region's raw JSON bytes plus its occurrence number, and **sticky for the session**: a region that
   was trimmed is trimmed the same way on every later call, and one that was left alone stays alone, so the bytes
   before the newest result are identical call after call. The per-region gain gate (`minGainPct`) is applied once,
   when the region is first seen. A deadline skip or an observe-mode first sight is also remembered as "unchanged".
   A mode change costs one accepted cache miss. `schema_trim` is the single exception to the protected tool
   definitions; it is opt-in (Aggressive, behind a confirm) and changes the prefix once.
4. **Observe never rewrites.** It measures what the stages would save from the same decisions, reports
   `BodyAfter = BodyBefore - saved`, and never reads the memo of an acted run.
5. **Fold and unfold.** The newest assistant turn repeating a tool call whose result was folded unfolds that result
   and every later result of the same call, and sets `Meta["trim.unfold"]` so the duplicate-call monitor does not
   count the repeat as waste. A retry with the same call id is not a repeat.
6. **Byte-based token estimate (R8).** `TrimTokens(delta, sent, input)` scales the call's measured input tokens by the
   share of bytes removed; no tokenizer is bundled. The saving is `certain` only on `openai.chat` with a measured
   input, and `likely` otherwise (Anthropic, or no usage). Money is `contract.MicroUSD`.
7. **Bounds.** Per session: 4096 decisions, 4 MiB, 1024 folded keys. Across sessions: the tollgate session bound and
   64 MiB, least recently used first. First writer wins on a race; a closed or evicted memo stores nothing.
8. **Privacy.** Only sizes, stage names, tool names and our fixed markers are stored (`trim_events`). The preview
   endpoint returns those; no request or result text is kept or logged.

## Consequences

- No cgo and no second process; the cost is that the stages are ported and tested here (golden fixtures per stage).
- Byte-identical prefixes across calls are a tested property (`TestSessionPrefixIsByteIdenticalAcrossCalls` in
  `internal/trim`, `TestChainSendsByteIdenticalPrefixesToTheUpstream` through the real chain and the upstream).
- A hook that is not adopted in time costs at most one cache miss.
- Estimates are not exact; they are labelled `likely` wherever the input is not measured.
- The Responses wire is not supported (`DEFERRED (R12)`): the hook passes it through untouched.
- **Enforced by:** `internal/archtest` H3 (purity), the bench gate (`TestBeforeP99UnderOneMillisecondOn100KB`,
  `TestAllocationsStayFlatOver10kCalls`), and the reconciliation tests in `internal/store/trim_test.go`.
