package store_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

func adminExec(t *testing.T, dsn string, stmts ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func asRole(ctx context.Context, t *testing.T, dsn, role string) *store.Store {
	t.Helper()
	u, _ := url.Parse(dsn)
	u.User = url.UserPassword(role, "pw")
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

// TestCheckRoleSeesSuperusersAndOwnersOfTheAuditLog runs the check as real roles. The roles are
// cluster-wide, so they are created first and dropped last, after the throwaway database.
func TestCheckRoleSeesSuperusersAndOwnersOfTheAuditLog(t *testing.T) {
	admin := pgtest.DSN(t)
	var b [4]byte
	_, _ = rand.Read(b[:])
	sfx := hex.EncodeToString(b[:])
	app, owner := "mw_app_"+sfx, "mw_owner_"+sfx
	adminExec(t, admin,
		"CREATE ROLE "+app+" LOGIN PASSWORD 'pw'",
		"CREATE ROLE "+owner+" NOLOGIN")
	t.Cleanup(func() {
		adminExec(t, admin, "DROP ROLE IF EXISTS "+app, "DROP ROLE IF EXISTS "+owner)
	})
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(context.Background(), dsn); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	owned := func() store.RoleCheck {
		rc, err := asRole(ctx, t, dsn, app).CheckRole(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return rc
	}
	su, err := mustOpen(t, dsn).CheckRole(ctx)
	if err != nil || !su.Superuser || !su.OwnsAuditLog || su.Role == "" {
		t.Fatalf("the migrating superuser: %+v, %v", su, err)
	}
	if rc := owned(); rc.Superuser || rc.OwnsAuditLog || rc.Role != app {
		t.Fatalf("a plain app role: %+v", rc)
	}
	adminExec(t, dsn, "ALTER TABLE audit_log OWNER TO "+owner, "GRANT "+owner+" TO "+app)
	if rc := owned(); rc.Superuser || !rc.OwnsAuditLog {
		t.Fatalf("a member of the owner role has the owner's rights and must count as owning: %+v", rc)
	}
	adminExec(t, dsn, "REVOKE "+owner+" FROM "+app)
	if rc := owned(); rc.OwnsAuditLog {
		t.Fatalf("after the revoke: %+v", rc)
	}
}

func mustOpen(t *testing.T, dsn string) *store.Store {
	t.Helper()
	db, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func TestCheckRoleOnAnUnmigratedDatabaseOwnsNothing(t *testing.T) {
	rc, err := mustOpen(t, pgtest.NewDatabase(t)).CheckRole(context.Background())
	if err != nil || rc.OwnsAuditLog {
		t.Fatalf("%+v, %v", rc, err)
	}
}
