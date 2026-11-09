// Package audit is the one writer of audit_log: a Sink that appends events and a reader that
// pages them. The table refuses updates and deletes (a trigger), so the log is append-only. The
// control plane emits events after the change committed; a failed append is logged and never
// fails the request.
package audit
