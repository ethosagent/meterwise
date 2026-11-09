package audit

import (
	"context"
	"log/slog"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// Sink appends audit events to Postgres and lists them. It implements contract.AuditSink.
type Sink struct {
	st  *store.Store
	org string
	now func() time.Time
}

var _ contract.AuditSink = (*Sink)(nil)

// New builds the sink for the one org; now may be nil (time.Now).
func New(st *store.Store, now func() time.Time) *Sink {
	if now == nil {
		now = time.Now
	}
	return &Sink{st: st, org: store.OrgID(), now: now}
}

// Append records e. An unknown action is refused (the list is fixed), a missing time is filled in
// and the org defaults to the one org. Nothing here updates or deletes a row.
func (s *Sink) Append(ctx context.Context, e contract.AuditEvent) error {
	if !e.Action.Valid() {
		return contract.NewError(contract.CodeInternal, "audit: unknown action")
	}
	if e.OrgID == "" {
		e.OrgID = s.org
	}
	if e.At.IsZero() {
		e.At = s.now()
	}
	return s.st.AppendAudit(ctx, e)
}

// List returns one page of the org's log, newest first.
func (s *Sink) List(ctx context.Context, q contract.AuditQuery) (contract.AuditPage, error) {
	if q.Action != "" && !q.Action.Valid() {
		return contract.AuditPage{}, contract.Invalid("action", "is not an audit action")
	}
	return s.st.ListAudit(ctx, s.org, q)
}

// Emit appends e and, if that fails, logs at ERROR and returns: an audit failure must not undo or
// fail the change that already committed. The log line carries the action and the error class
// only, never the event's metadata, ids of the workspace URL or the client address.
func Emit(ctx context.Context, log *slog.Logger, sink contract.AuditSink, e contract.AuditEvent) {
	if sink == nil {
		return
	}
	if err := sink.Append(ctx, e); err != nil && log != nil {
		log.LogAttrs(ctx, slog.LevelError, "audit: append failed", slog.String("action", string(e.Action)), slog.String("code", string(contract.AsError(err).Code)))
	}
}
