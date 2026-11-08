# Context budget in this repo

This repo follows Mitesh's **Context Budget Playbook**: a coding agent reasons well only while its context
is small. This file maps each rule to **what enforces it here**. A rule only the agent remembers is lost at
the first compaction, so each one is a gate, a hook or a line in the 150-line map.

## Size caps (source of truth: `size_limit`, `MAX_SKILLS`, `MAX_SKILL_DESC` in `scripts/lib.sh`)

| File | Cap (lines) | Enforced by |
|---|---|---|
| `CLAUDE.md` = `AGENTS.md` | 150 | `size` gate, post-edit hook, Stop hook |
| `ARCHITECTURE.md` | 300 | same |
| Any doc in `docs/`, `.agents/`, root `*.md`, runbooks | 200 | same (`docs/schema.md` is generated, uncapped) |
| ADR / ADR index | 120 / 150 | same |
| `SKILL.md` / skill `references/*.md` | 300 / 200 | same; description at most 250 chars: `docs` gate |
| `.claude/rules/*.md` | 60 | same |
| Web feature `README.md` | 120 | same |
| Go source file | 300 soft (warn), 800 hard | same; whole-file Read blocked over 400 |
| Go test file | 500 | same |
| TS/TSX source file | 250 soft (warn), 400 hard | same |
| TS test file | 500 | same |
| Function | 50 lines, cognitive complexity 20 | `lint` gate (`funlen`, `gocognit`) |
| Project skills | fewer than 40 | `size` gate |
| Coverage floor | Go 75 %, web 70 % | `test` gate (`COVERAGE_MIN`), `web` gate (vitest thresholds) |

## Hooks (`.claude/settings.json`, scripts in `.claude/hooks/`)

| Hook | What it does deterministically |
|---|---|
| SessionStart | Prints branch, dirty count, newest `plan/*.md` (if `plan/` exists), `HANDOFF.md`; on startup a model-and-effort reminder; after compaction the non-negotiables |
| PreToolUse Read | Blocks whole-file reads over 400 lines, and any read of generated files or lockfiles; optional local reader model via `METERWISE_READER_URL` |
| PreToolUse Bash | Best-effort: blocks raw `go test`/`go vet`/`golangci-lint`/`govulncheck`/`npm test`/`vitest`/`biome`, names `scripts/quiet.sh` |
| PostToolUse Edit/Write | `gofmt -w` Go, `biome format --write` TS/TSX, syncs `CLAUDE.md` and `AGENTS.md`, reports a file over its cap |
| Stop | When the tree is dirty runs `run-checks.sh --fast`; a failure blocks finishing and returns the findings |

The `env` block in `.claude/settings.json` sets the output caps and the compaction backstop (400K window, 15K tool output).

## Quiet tooling

- `scripts/run-checks.sh` prints failures only; full logs in `.logs/<gate>.log`; `--json` adds a summary line.
- `scripts/quiet.sh <cmd>`: one line on pass, filtered failures plus the log path on fail.
- Deny-read rules keep `go.sum`, lockfiles, coverage output, `web/dist` and generated files out of context.

## Session habits (not enforceable; keep them)

- One task per session. Multi-file work starts with `plan-task`; implement it in a fresh session.
- Subagents return at most 2K tokens with `file:line`; `.claude/agents/reader.md` does bulk reads.
- Hand off before about 200K tokens (`handoff` skill); never rely on auto-compaction.
- Pick model and effort once per session; switching mid-session breaks the prompt cache.
- Re-measure the baseline monthly: [`runbooks/agent-hygiene.md`](runbooks/agent-hygiene.md).
