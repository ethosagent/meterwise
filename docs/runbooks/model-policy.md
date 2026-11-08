# Model policy

Plan big, execute small. Pick the model and effort once per session; switching mid-session breaks the prompt cache.

| Work | Model tier | Why |
|---|---|---|
| Plans, architecture, ADRs, security review, cross-layer changes, anything touching `internal/contract` or `ARCHITECTURE.md` | Opus (or Fable) | one careful pass saves many rework turns |
| Implementing a written plan, tests, mechanical refactors, docs | Sonnet | the plan already holds the judgement |
| Bulk reads, grep sweeps, summaries, log triage | Haiku, or a local model | cheap, and the output is a short summary |

## Rules

- Planning happens in one context and ends in a file (`plan-task`); implementation starts in a fresh context from that file.
- Subagents return at most 2K tokens with `file:line`; longer output goes to a file. `.claude/agents/reader.md` is the bulk reader.
- State the model and why when launching a subagent.
- Review lenses by tier (`.agents/pipeline.yaml`) are run by a model that did not write the change when possible.

## Optional local reader

If `METERWISE_READER_URL` points at an OpenAI-compatible server (for example a local model), `read-guard.sh` sends a
blocked large-file read to it and returns the summary instead of a bare refusal. Default off. Secret-like files
(`.env`, keys, anything named secret, credential or token) are never sent. `METERWISE_READER_MODEL` picks the model.
