package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// Audit page sizes: a page holds DefaultAuditLimit rows unless the caller asks for fewer or more,
// never more than contract.MaxAuditLimit.
const DefaultAuditLimit = 50

// AppendAudit inserts one audit row. It is the only INSERT into audit_log (internal/audit calls it);
// the table refuses updates and deletes, so nothing here can change a row once written.
func (s *Store) AppendAudit(ctx context.Context, e contract.AuditEvent) error {
	meta, err := json.Marshal(nonNilMap(e.Metadata))
	if err != nil {
		return fail(ClassPool, "store: audit metadata is not JSON")
	}
	at := e.At
	if at.IsZero() {
		at = time.Now()
	}
	return queryErr(s.q.InsertAudit(ctx, sqlc.InsertAuditParams{
		OrgID: e.OrgID, WorkspaceID: e.WorkspaceID, ActorUserID: e.ActorID, Action: string(e.Action), ResourceType: e.ResourceType,
		ResourceID: e.ResourceID, Ip: e.IP, Metadata: meta, CreatedAt: pgts(at),
	}))
}

// nonNilMap is the one nil-to-{} normalisation: an audit row's metadata, a workspace's settings and
// every JSON object read back from the database go through it.
func nonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// ListAudit returns one page of the org's audit log, newest first by (created_at, id). An invalid
// cursor, workspace id or limit is a validation error; the limit defaults to DefaultAuditLimit.
func (s *Store) ListAudit(ctx context.Context, orgID string, q contract.AuditQuery) (contract.AuditPage, error) {
	limit := q.Limit
	switch {
	case limit == 0:
		limit = DefaultAuditLimit
	case limit < contract.MinPageLimit || limit > contract.MaxAuditLimit:
		return contract.AuditPage{}, contract.Invalid("limit", contract.LimitMessage)
	}
	if q.WorkspaceID != "" && !validID(q.WorkspaceID) {
		return contract.AuditPage{}, contract.Invalid("workspaceId", "must be a workspace id")
	}
	at, id, err := decodeAuditCursor(q.Cursor)
	if err != nil {
		return contract.AuditPage{}, err
	}
	rows, err := s.auditRows(ctx, orgID, q, limit+1, at, id)
	if err != nil {
		return contract.AuditPage{}, queryErr(err)
	}
	page := contract.AuditPage{Items: make([]contract.AuditRecord, 0, min(len(rows), limit))}
	for i, r := range rows {
		if i == limit { // the extra row only proves there is a next page
			last := page.Items[limit-1]
			page.NextCursor = encodeAuditCursor(last.At, last.ID)
			break
		}
		page.Items = append(page.Items, contract.AuditRecord{
			ID: r.ID, At: ts(r.CreatedAt), Action: contract.AuditAction(r.Action), ResourceType: r.ResourceType, ResourceID: r.ResourceID,
			WorkspaceID: r.WorkspaceID, ActorID: r.ActorUserID, ActorEmail: r.ActorEmail, ActorName: r.ActorName, IP: r.Ip,
			Metadata: settingsOf(r.Metadata),
		})
	}
	return page, nil
}

// auditRows runs the first-page query, or the keyset query when the caller holds a cursor (id > 0),
// so the planner never sees a "cursor_id = 0 OR ..." predicate.
func (s *Store) auditRows(ctx context.Context, orgID string, q contract.AuditQuery, maxRows int, at time.Time, id int64) ([]sqlc.ListAuditAfterRow, error) {
	if id == 0 {
		first, err := s.q.ListAuditFirst(ctx, sqlc.ListAuditFirstParams{
			OrgID: orgID, Action: string(q.Action), WorkspaceID: q.WorkspaceID, MaxRows: clampInt32(int64(maxRows)),
		})
		if err != nil {
			return nil, err
		}
		rows := make([]sqlc.ListAuditAfterRow, len(first))
		for i, r := range first {
			rows[i] = sqlc.ListAuditAfterRow(r)
		}
		return rows, nil
	}
	return s.q.ListAuditAfter(ctx, sqlc.ListAuditAfterParams{
		OrgID: orgID, Action: string(q.Action), WorkspaceID: q.WorkspaceID, CursorID: id, CursorAt: pgts(at), MaxRows: clampInt32(int64(maxRows)),
	})
}

// The cursor is "<unix micro>.<id>" in URL-safe base64: opaque to clients, exact for Postgres's
// microsecond timestamps.
func encodeAuditCursor(at time.Time, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d.%d", at.UnixMicro(), id)))
}

func decodeAuditCursor(c string) (time.Time, int64, error) {
	if c == "" {
		return time.Time{}, 0, nil
	}
	bad := contract.Invalid("cursor", "is not a cursor this API issued")
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, 0, bad
	}
	us, idStr, ok := strings.Cut(string(raw), ".")
	micro, err1 := strconv.ParseInt(us, 10, 64)
	id, err2 := strconv.ParseInt(idStr, 10, 64)
	if !ok || err1 != nil || err2 != nil || id <= 0 {
		return time.Time{}, 0, bad
	}
	return time.UnixMicro(micro).UTC(), id, nil
}
