#!/usr/bin/env bash
# Point git at .githooks for THIS working tree only. Run once per clone or worktree.
#
# The hooks path is written with `git config --worktree` (enables
# extensions.worktreeConfig in the repository config, which is harmless to other
# worktrees), so a sibling worktree of the same repository is not affected. Global
# and system git config are never touched.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
chmod +x .githooks/* scripts/*.sh .claude/hooks/*.sh
git config extensions.worktreeConfig true
git config --worktree core.hooksPath .githooks
echo "git hooks installed for $(pwd): pre-commit (fmt vet docs), pre-push (scripts/run-checks.sh)"
