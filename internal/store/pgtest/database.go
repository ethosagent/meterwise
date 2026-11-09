package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// NewDatabase creates a throwaway database on the loopback server named by
// TEST_DATABASE_URL and returns its DSN. The database is dropped when the test
// ends, so tests never share state and never touch the maintenance database.
// connect_timeout and search_path are not in the DSN (ruling R20): callers set
// them in code.
func NewDatabase(t testing.TB) string {
	t.Helper()
	admin := DSN(t)
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatalf("pgtest: %v", err)
	}
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("pgtest: %v", err)
	}
	name := "mw_test_" + hex.EncodeToString(b[:])
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exec(ctx, t, admin, "CREATE DATABASE "+name)
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		exec(cctx, t, admin, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	u.Path = "/" + name
	return u.String()
}

func exec(ctx context.Context, t testing.TB, dsn, stmt string) {
	t.Helper()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("pgtest: %v", err)
	}
	cfg.ConnectTimeout = 5 * time.Second
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pgtest: connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, stmt); err != nil {
		t.Fatalf("pgtest: %s: %v", stmt, err)
	}
}
