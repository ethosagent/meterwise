# 0001. One binary serves the proxy and the control plane

- **Status:** Accepted
- **Date:** 2026-11-09
- **Source:** Plan 00 D1

## Context

tollgate is Go. A separate API service would add a deploy, a second Postgres pool and a network hop between the proxy and the control plane. Options: one binary (chosen), two services, a sidecar proxy.

## Decision

`cmd/meterwised` serves the tollgate handler and the `/api` control plane in one process with one Postgres pool. Split later only on measured need.

## Consequences

One deploy and shared config. A control-plane fault shares a process with the proxy, so the hook and queue rules (no I/O in `Before`, bounded queues) carry the isolation. Enforced by: H2 layering, `STR-*` lens, `cmd/meterwised` as the only composition root.
