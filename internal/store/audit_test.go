package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

func auditRig(t *testing.T) (*store.Store, *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	return s, conn
}

func appendN(t *testing.T, s *store.Store, n int, action contract.AuditAction, ws string, base time.Time) {
	t.Helper()
	for i := range n {
		e := contract.AuditEvent{At: base.Add(time.Duration(i) * time.Second), OrgID: store.OrgID(), Action: action, ResourceType: "workspace", ResourceID: ws, WorkspaceID: ws, IP: "203.0.113.9", Metadata: map[string]any{"i": i}}
		if err := s.AppendAudit(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
}

// The database itself refuses to change or remove a row, whatever role connects (the test role is
// the owner): each statement fails with the trigger's message, and the row survives.
func TestAuditLogRefusesUpdateDeleteAndTruncate(t *testing.T) {
	ctx := context.Background()
	s, conn := auditRig(t)
	appendN(t, s, 2, contract.AuditWorkspaceCreate, "", time.Now())
	for name, stmt := range map[string]string{
		"UPDATE":                  `UPDATE audit_log SET action = 'x'`,
		"UPDATE of no row":        `UPDATE audit_log SET action = 'x' WHERE id < 0`,
		"DELETE":                  `DELETE FROM audit_log`,
		"DELETE of no row":        `DELETE FROM audit_log WHERE id < 0`,
		"TRUNCATE":                `TRUNCATE audit_log`,
		"TRUNCATE CASCADE":        `TRUNCATE audit_log CASCADE`,
		"UPDATE of the timestamp": `UPDATE audit_log SET created_at = now() - interval '1 year'`,
	} {
		_, err := conn.Exec(ctx, stmt)
		if err == nil || !strings.Contains(err.Error(), "audit_log is append-only") {
			t.Errorf("%s: %v", name, err)
		}
	}
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("rows after the refused statements: %d %v", n, err)
	}
	// INSERT stays allowed, and the second wall: the privileges are revoked from PUBLIC.
	if _, err := conn.Exec(ctx, `INSERT INTO audit_log (org_id, action, resource_type) VALUES ($1, 'workspace.create', 'workspace')`, store.OrgID()); err != nil {
		t.Fatal(err)
	}
	var grants int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM information_schema.role_table_grants
WHERE table_name = 'audit_log' AND grantee = 'PUBLIC' AND privilege_type IN ('UPDATE', 'DELETE', 'TRUNCATE')`).Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("PUBLIC still holds %d change privilege(s): %v", grants, err)
	}
}

func TestAuditAppendStoresEverythingAndListsNewestFirstWithCursor(t *testing.T) {
	ctx, org := context.Background(), store.OrgID()
	s, _ := auditRig(t)
	u, err := s.UpsertUser(ctx, org, contract.Identity{Subject: "user_actor", Email: "actor@corp.test", EmailVerified: true, Name: "Act Or"})
	if err != nil {
		t.Fatal(err)
	}
	ws := "11111111-1111-1111-1111-111111111111"
	base := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	appendN(t, s, 5, contract.AuditWorkspaceUpdate, ws, base)
	if err := s.AppendAudit(ctx, contract.AuditEvent{At: base.Add(time.Minute), OrgID: org, ActorID: u.ID, Action: contract.AuditMemberRemove, ResourceType: "member", ResourceID: u.ID, Metadata: map[string]any{"role": "member"}}); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListAudit(ctx, org, contract.AuditQuery{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatalf("first page: %+v %v", page, err)
	}
	top := page.Items[0]
	if top.Action != contract.AuditMemberRemove || top.ActorEmail != "actor@corp.test" || top.ActorName != "Act Or" || top.ActorID != u.ID ||
		top.WorkspaceID != "" || top.IP != "" || top.Metadata["role"] != "member" || top.ResourceID != u.ID {
		t.Fatalf("top row: %+v", top)
	}
	second := page.Items[1]
	if second.IP != "203.0.113.9" || second.WorkspaceID != ws || second.ActorID != "" || !second.At.Equal(base.Add(4*time.Second)) {
		t.Fatalf("second row: %+v", second)
	}
	var seen []int64
	cursor := ""
	for range 10 {
		p, err := s.ListAudit(ctx, org, contract.AuditQuery{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range p.Items {
			seen = append(seen, it.ID)
		}
		if cursor = p.NextCursor; cursor == "" {
			break
		}
	}
	if len(seen) != 6 {
		t.Fatalf("paging saw %d rows, want 6: %v", len(seen), seen)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] == seen[i-1] {
			t.Fatalf("a row repeated across pages: %v", seen)
		}
	}
	only, err := s.ListAudit(ctx, org, contract.AuditQuery{Action: contract.AuditWorkspaceUpdate, WorkspaceID: ws, Limit: 200})
	if err != nil || len(only.Items) != 5 || only.NextCursor != "" {
		t.Fatalf("filters: %+v %v", only, err)
	}
	none, err := s.ListAudit(ctx, "00000000-0000-0000-0000-0000000000ff", contract.AuditQuery{})
	if err != nil || none.Items == nil || len(none.Items) != 0 {
		t.Fatalf("another org sees nothing: %+v %v", none, err)
	}
	for name, q := range map[string]contract.AuditQuery{
		"garbage cursor": {Cursor: "!!"}, "cursor of the wrong shape": {Cursor: "YWJj"}, "negative limit": {Limit: -1},
		"limit too large": {Limit: 201}, "workspace id that is not one": {WorkspaceID: "nope"},
	} {
		if _, err := s.ListAudit(ctx, org, q); code(err) != contract.CodeValidationFailed {
			t.Errorf("%s: %v", name, err)
		}
	}
}
