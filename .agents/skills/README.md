# Skills

Tool-agnostic home for this repo's agent skills. `.claude/skills` is a symlink
here, so there is one copy. Skills are for **recurring procedures** only;
one-off knowledge goes in `docs/`.

| Skill | Use it to |
|---|---|
| [`add-migration`](add-migration/SKILL.md) | add a goose migration (one feature per file), regenerate sqlc and `docs/schema.md` |
| [`add-endpoint`](add-endpoint/SKILL.md) | add a route-to-permission row, DTO, handler, query, test, OpenAPI and web type |
| [`add-monitor`](add-monitor/SKILL.md) | add a waste monitor with ring fixtures, savings rule, setting and dashboard row |
| [`add-page`](add-page/SKILL.md) | add a web feature slice with a permission guard |
| [`add-tollgate-ext`](add-tollgate-ext/SKILL.md) | wire a tollgate extension into the data plane with a testkit test |
| [`demo-traffic`](demo-traffic/SKILL.md) | replay synthetic fixtures through a local workspace (manual only) |
| [`write-adr`](write-adr/SKILL.md) | record a decision that moves a boundary |
| [`plan-task`](plan-task/SKILL.md) | write `plan/NN-title.md` before coding a multi-file task |
| [`handoff`](handoff/SKILL.md) | write `HANDOFF.md` before a long session ends (manual only) |

A `SKILL.md` starts with frontmatter: `name` equal to its directory and a
`description` of at most 250 characters (`scripts/check-docs.sh`). `demo-traffic` and
`handoff` carry `disable-model-invocation: true` so they stay out of the model's listing.
Skills defer to `CLAUDE.md` for rules instead of restating them. Put the rules that must
never be missed in the first 60 lines, and ship mechanical steps as `scripts/*.sh` next to
the `SKILL.md`; running a script costs a few lines of output, reasoning through the same
step costs thousands of tokens.
