# Parallel sessions

One plan per worktree, one agent per plan. The orchestrator holds only the plan list, never the code.

## Create a worktree

```sh
make worktree PLAN=05            # git worktree add ../meterwise-05 -b wave/05 ; installs hooks there
cd ../meterwise-05 && make check   # same gate as CI, scoped to this tree
```

`plan/` is gitignored and absent in a new worktree. Give the agent the plan's absolute path, or copy the one file in.

## Rules

- Never share a branch between two worktrees; never work in another plan's worktree.
- Each tree has its own `.logs/`, `web/node_modules/` and git hooks (`scripts/install-hooks.sh` sets `core.hooksPath` locally).
- The compose Postgres is shared (project name `meterwise`); gates use throwaway databases, so trees do not collide.
- Merge order follows the plan map; resolve a conflict in the tree that merges second.
- Remove a finished tree: `git worktree remove ../meterwise-05` after the branch is merged.
- Agents never push, tag or merge to `main`; a human does.
