package ledger_test

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

// A real Postgres that refuses connections for about 3 s (ALLOW_CONNECTIONS false, backends terminated:
// SQLSTATE 55000 and 57P01 server errors, not dial errors) must cost the ledger nothing: every call
// and every session close is written once it is back.
func TestRealPostgresOutageOfThreeSecondsLosesNoCallAndNoClose(t *testing.T) {
	s, ws := pgStore(t)
	dsn := s.Pool().Config().ConnString()
	u, _ := url.Parse(dsn)
	dbName := strings.TrimPrefix(u.Path, "/")
	admin := pgtest.DSN(t)
	adminExec(t, admin, fmt.Sprintf("ALTER DATABASE %s ALLOW_CONNECTIONS true", dbName)) // fails early if not permitted
	t.Cleanup(func() { adminExec(t, admin, fmt.Sprintf("ALTER DATABASE %s ALLOW_CONNECTIONS true", dbName)) })

	h := newHook(t, s, nil) // default retry policy
	cut := func() {
		adminExec(t, admin, fmt.Sprintf("ALTER DATABASE %s ALLOW_CONNECTIONS false", dbName),
			fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()", dbName))
	}
	cut()
	restore := time.AfterFunc(3*time.Second, func() {
		adminExec(t, admin, fmt.Sprintf("ALTER DATABASE %s ALLOW_CONNECTIONS true", dbName))
	})
	defer restore.Stop()

	const calls, sessions = 70, 5
	for i := range calls {
		c := directCall(fmt.Sprintf("out-%03d", i), "hi", nil, i/sessions+1)
		c.Tenant.ID, c.Session.TenantID = ws, ws
		c.Session.ID = fmt.Sprintf("sess-%d", i%sessions)
		complete(t, h, c)
		time.Sleep(40 * time.Millisecond)
	}
	for k := range sessions {
		h.OnSessionClosed(context.Background(), tollgate.Session{ID: fmt.Sprintf("sess-%d", k), TenantID: ws})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := h.Close(ctx); err != nil {
		t.Fatalf("close: %v (status %v)", err, h.Status())
	}
	st := h.Status()
	if n := scalar[int](t, s, `SELECT count(*) FROM calls WHERE workspace_id = $1`, ws); n != calls {
		t.Fatalf("rows = %d, want %d; status %v", n, calls, st)
	}
	if n := scalar[int](t, s, `SELECT count(*) FROM sessions WHERE workspace_id = $1 AND closed_at IS NOT NULL`, ws); n != sessions {
		t.Fatalf("closed sessions = %d, want %d; status %v", n, sessions, st)
	}
	if st["lost"] != int64(0) || st["lost_closes"] != int64(0) || st["dropped"] != int64(0) {
		t.Fatalf("status = %v", st)
	}
}

func adminExec(t testing.TB, dsn string, stmts ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pgtest.Exec(ctx, dsn, stmts...); err != nil {
		t.Errorf("admin: %v", err)
	}
}

// The store's count of unreadable tool_calls values is on /statusz through the ledger's status.
func TestStatusCarriesTheStoresUnreadableToolCallsCount(t *testing.T) {
	s, _ := pgStore(t)
	h := newHook(t, s, nil)
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	if v, ok := h.Status()["unreadable_tool_calls"]; !ok || v != int64(0) {
		t.Fatalf("status = %v", h.Status())
	}
	if _, ok := newHook(t, &fakeWriter{}, nil).Status()["unreadable_tool_calls"]; ok {
		t.Fatal("a writer that cannot count them must not report a count")
	}
}
