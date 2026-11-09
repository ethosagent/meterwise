# 0018. The audit log is append-only by trigger and emitted after commit

- **Status:** Accepted
- **Date:** 2026-11-10
- **Source:** plan/04-workspace-url-setup-and-audit-log.md, 04-interface.md

## Context

An audit log that its own writer can edit proves little, and one written before a change commits
records things that did not happen. A failed audit write must not undo a change a user already
made, but it must not be silent either.

## Decision

1. **Append-only in the database.** `audit_log` has a `BEFORE UPDATE OR DELETE OR TRUNCATE`
   statement trigger that raises, and `UPDATE`, `DELETE`, `TRUNCATE` are revoked from `PUBLIC`.
   The trigger holds for the table owner too, so a code bug cannot rewrite history. Retention would
   be a deliberate migration that drops the trigger, never a silent `DELETE`.
2. **One writer.** Only `internal/audit` (through `store.AppendAudit` and `queries/audit.sql`) inserts
   (archtest H4). The action list is fixed in `contract.AuditActions()` and mirrored as `AUDIT_ACTIONS`
   in the web twin; an unknown action is refused on append and is 422 on the read filter.
3. **Emitted after commit** by the API handler, with the actor, the TCP peer address and small
   non-sensitive metadata (a role, a count, a field name). If `Append` fails the handler logs ERROR
   (action and error code only) and still succeeds.
4. **No foreign keys** from `audit_log` to workspaces or users, so deleting either never touches the
   log; the read joins users for the actor's name and email when the user still exists.
5. **Read**: `GET /api/audit-log`, `audit_log:read` (org scope, not seeded, so admins only by
   default), newest first by `(created_at, id)`, opaque cursor, default 50, max 200.

## Consequences

Easier: a reviewer can trust that rows were never edited. Harder: an audit write failing after
commit leaves a missing row (logged, not retried); a transactional outbox would close it and is not
built. Enforced by `TestAuditLogRefusesUpdateDeleteAndTruncate`, `TestAnAuditFailureDoesNotFailTheChange`.
