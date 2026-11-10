---
name: demo-traffic
description: Fill a workspace's ledger with synthetic sessions (OpenAI and Anthropic wires) so the dashboard, Setup status and /stats have data. Manual only; run when a demo or screenshot needs traffic.
disable-model-invocation: true
---

# Demo traffic

Rules first:
- Manual only. Needs a migrated Postgres in `DATABASE_URL`; no running `meterwised` is needed.
- `cmd/demo-traffic` runs a real tollgate proxy with the ledger hook against a loopback fake upstream built into the command. No provider key exists anywhere in the run, and it never contacts a live provider.
- Bodies are synthetic; never replay real prompts.

## Steps

1. `make db-up && go run ./cmd/meterwised migrate` (or `make up` for the Docker stack).
2. `make demo-traffic` (native `DATABASE_URL`), or `make docker-demo-traffic` (the Docker stack database).
   `ROUNDS=20 WORKSPACE=<workspace uuid> make demo-traffic` picks the volume and the workspace (default: the default workspace).
3. Open Setup: "connected - last call ... - N calls today"; `GET /api/workspaces/{id}/stats` shows spend.
