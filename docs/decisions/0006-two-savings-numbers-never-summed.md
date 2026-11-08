# 0006. Saved and likely saved are two numbers, never summed

- **Status:** Accepted
- **Date:** 2026-11-09
- **Source:** Plan 00 D11

## Context

A blocked call has a certain price; a shorter session is an estimate. Adding them would make the headline unauditable. Options: one total, two numbers (chosen).

## Decision

The ledger, API, charts and copy keep **saved** (blocked calls) and **likely saved** (estimated) as separate fields. Every headline dollar is auditable to one call.

## Consequences

Trust over a bigger number. DTOs have two fields and no total. Enforced by: `DATA-3`, the savings tests, code review of any new aggregate.
