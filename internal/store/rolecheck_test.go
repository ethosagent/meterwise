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

// TestCheckRoleSeesMembershipOfSuperuserAndBypassRLSRoles: a role that is not itself a superuser
// but is a member of one can SET ROLE to it, so it must report Superuser; the same holds for
// BYPASSRLS, directly or through membership.
func TestCheckRoleSeesMembershipOfSuperuserAndBypassRLSRoles(t *testing.T) {
	admin := pgtest.DSN(t)
	var b [4]byte
	_, _ = rand.Read(b[:])
	sfx := hex.EncodeToString(b[:])
	app, su, byp, direct := "mw_app_"+sfx, "mw_su_"+sfx, "mw_byp_"+sfx, "mw_direct_"+sfx
	adminExec(t, admin,
		"CREATE ROLE "+app+" LOGIN PASSWORD 'pw'",
		"CREATE ROLE "+direct+" LOGIN PASSWORD 'pw' BYPASSRLS",
		"CREATE ROLE "+su+" NOLOGIN SUPERUSER",
		"CREATE ROLE "+byp+" NOLOGIN BYPASSRLS")
	t.Cleanup(func() {
		adminExec(t, admin, "DROP ROLE IF EXISTS "+app, "DROP ROLE IF EXISTS "+direct,
			"DROP ROLE IF EXISTS "+su, "DROP ROLE IF EXISTS "+byp)
	})
	dsn := pgtest.NewDatabase(t)
	ctx := context.Background()
	check := func(role string) store.RoleCheck {
		rc, err := asRole(ctx, t, dsn, role).CheckRole(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return rc
	}
	if rc := check(app); rc.Superuser || rc.BypassRLS {
		t.Fatalf("a plain app role: %+v", rc)
	}
	if rc := check(direct); rc.Superuser || !rc.BypassRLS {
		t.Fatalf("a direct BYPASSRLS role: %+v", rc)
	}
	adminExec(t, dsn, "GRANT "+su+" TO "+app)
	if rc := check(app); !rc.Superuser {
		t.Fatalf("a member of a superuser role must count as a superuser: %+v", rc)
	}
	adminExec(t, dsn, "REVOKE "+su+" FROM "+app, "GRANT "+byp+" TO "+app)
	if rc := check(app); rc.Superuser || !rc.BypassRLS {
		t.Fatalf("a member of a BYPASSRLS role: %+v", rc)
	}
	adminExec(t, dsn, "REVOKE "+byp+" FROM "+app)
	if rc := check(app); rc.Superuser || rc.BypassRLS {
		t.Fatalf("after the revokes: %+v", rc)
	}
}

// TestCheckRoleFindsAuditLogWhateverTheSearchPath: a DSN whose search_path leaves out the schema
// that holds audit_log must not make the guard fail open.
func TestCheckRoleFindsAuditLogWhateverTheSearchPath(t *testing.T) {
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(context.Background(), dsn); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", "pg_catalog")
	u.RawQuery = q.Encode()
	rc, err := mustOpen(t, u.String()).CheckRole(context.Background())
	if err != nil || !rc.OwnsAuditLog {
		t.Fatalf("owner of audit_log outside the search_path: %+v, %v", rc, err)
	}
}

// TestCheckRoleRefusesANonInheritingMemberOfTheOwner: a role granted the audit_log owner WITH INHERIT
// FALSE (or one with the NOINHERIT attribute) does not inherit the owner's privileges, but it can
// SET ROLE to the owner and then drop the append-only trigger. It must still count as owning.
func TestCheckRoleRefusesANonInheritingMemberOfTheOwner(t *testing.T) {
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
	check := func() store.RoleCheck {
		rc, err := asRole(ctx, t, dsn, app).CheckRole(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return rc
	}
	adminExec(t, dsn, "ALTER TABLE audit_log OWNER TO "+owner, "GRANT "+owner+" TO "+app+" WITH INHERIT FALSE")
	if rc := check(); !rc.OwnsAuditLog {
		t.Fatalf("GRANT owner TO app WITH INHERIT FALSE: the role can SET ROLE to the owner and must count as owning: %+v", rc)
	}
	adminExec(t, dsn, "REVOKE "+owner+" FROM "+app, "ALTER ROLE "+app+" NOINHERIT", "GRANT "+owner+" TO "+app)
	if rc := check(); !rc.OwnsAuditLog {
		t.Fatalf("a NOINHERIT role that is a member of the owner must count as owning: %+v", rc)
	}
	adminExec(t, dsn, "REVOKE "+owner+" FROM "+app)
	if rc := check(); rc.OwnsAuditLog {
		t.Fatalf("after the revoke: %+v", rc)
	}
}
