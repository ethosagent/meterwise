package store_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"testing"

	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

func roleDSN(dsn, role string) string {
	u, _ := url.Parse(dsn)
	u.User = url.UserPassword(role, "pw")
	return u.String()
}

// TestMigrateAsNonSuperuserOwnerWithPreCreatedVector is the production shape of docs/runbooks/database-roles.md: pgvector
// is not a trusted extension, so a superuser creates it once per database; after that a non-superuser owner role runs
// every migration (0024 included), and the non-superuser app role works the trace tables and the serve role guard still
// passes for it (ADR 0031). Without the extension the same migrate fails at 0024.
func TestMigrateAsNonSuperuserOwnerWithPreCreatedVector(t *testing.T) {
	admin := pgtest.DSN(t)
	var b [4]byte
	_, _ = rand.Read(b[:])
	sfx := hex.EncodeToString(b[:])
	owner, app := "mw_own_"+sfx, "mw_app_"+sfx
	adminExec(t, admin, "CREATE ROLE "+owner+" LOGIN PASSWORD 'pw'", "CREATE ROLE "+app+" LOGIN PASSWORD 'pw'")
	t.Cleanup(func() { adminExec(t, admin, "DROP ROLE IF EXISTS "+app, "DROP ROLE IF EXISTS "+owner) })
	ctx := context.Background()
	prepare := func(dsn string) {
		adminExec(t, dsn, "REVOKE ALL ON SCHEMA public FROM PUBLIC", "GRANT USAGE ON SCHEMA public TO "+app, "GRANT USAGE, CREATE ON SCHEMA public TO "+owner,
			"ALTER DEFAULT PRIVILEGES FOR ROLE "+owner+" IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO "+app,
			"ALTER DEFAULT PRIVILEGES FOR ROLE "+owner+" IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO "+app)
	}

	// Without the extension a non-superuser owner cannot migrate: pgvector is not trusted.
	bare := pgtest.NewDatabase(t)
	prepare(bare)
	if _, err := store.Migrate(ctx, roleDSN(bare, owner)); err == nil {
		t.Fatal("migrate as a non-superuser owner must fail at 0024 while the extension is missing")
	}

	dsn := pgtest.NewDatabase(t)
	prepare(dsn)
	adminExec(t, dsn, "CREATE EXTENSION IF NOT EXISTS vector") // the runbook's one-time superuser step
	if _, err := store.Migrate(ctx, roleDSN(dsn, owner)); err != nil {
		t.Fatalf("migrate as a non-superuser owner with the extension pre-created: %v", err)
	}
	if rc, err := asRole(ctx, t, dsn, owner).CheckRole(ctx); err != nil || rc.Superuser || !rc.OwnsAuditLog {
		t.Fatalf("the owner role: %+v %v", rc, err)
	}

	// The serve role: not a superuser, not an owner, and it can store and recall traces.
	appDB := asRole(ctx, t, dsn, app)
	if rc, err := appDB.CheckRole(ctx); err != nil || rc.Superuser || rc.OwnsAuditLog || rc.Role != app {
		t.Fatalf("serve's role guard must pass for the app role: %+v %v", rc, err)
	}
	ws := newWS(t, appDB, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	if _, ok, err := appDB.InsertTrace(ctx, newTrace(ws, "s1", unit(0), 0.9)); err != nil || !ok {
		t.Fatalf("the app role inserts a trace: %v %v", ok, err)
	}
	if got, err := appDB.NearestTraces(ctx, ws, unit(0), 3, 0.6); err != nil || len(got) != 1 {
		t.Fatalf("the app role recalls it: %v %+v", err, got)
	}
}
