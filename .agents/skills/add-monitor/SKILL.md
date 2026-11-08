---
name: add-monitor
description: Add a waste-detecting monitor: detector, positive and negative ring fixtures, savings rule, settings default and dashboard row. Use when Plan 06's monitors package exists.
---

# Add a monitor

Rules first:
- `internal/monitors` is **pure** (archtest H3): stdlib, tollgate root, `internal/contract` only. No I/O, no clock reads (inject).
- A monitor is a hook check on the per-session history ring; modes are `observe | note | block`, per workspace.
- It must come with a **positive** fixture (fires) and a **negative** one (stays quiet) before the detector is written.
- A block is certain savings; anything estimated is "likely saved". They are never summed (DATA-3).
- Per-call code needs a `Benchmark*` (HOT-8).

## Steps

1. `.agents/skills/add-monitor/scripts/new-monitor.sh <name>` creates the two ring fixtures under
   `internal/monitors/testdata/<name>/` and prints the checklist (fails if the package does not exist yet).
2. Write the failing table test over both fixtures, then the detector `internal/monitors/<name>.go`.
3. Add the savings rule in `internal/savings`, the default in the workspace settings, and the dashboard row.
4. Review with `hot-path`, `data-integrity`, `test-quality` (critical tier).
