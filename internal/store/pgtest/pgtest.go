package pgtest

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/nonet"
)

// EnvDSN is the variable that carries the test database URL.
const EnvDSN = "TEST_DATABASE_URL"

// DSN returns the test database URL. Unset: under CI=true the test fails,
// otherwise it is skipped with a message that names the variable.
func DSN(t testing.TB) string {
	t.Helper()
	dsn := os.Getenv(EnvDSN)
	if dsn == "" {
		if os.Getenv("CI") == "true" {
			t.Fatalf("pgtest: %s is unset under CI=true; the Postgres service must be configured", EnvDSN)
		}
		t.Skipf("pgtest: %s is unset; start docker compose (make db-up) and export it to run database tests", EnvDSN)
	}
	if err := CheckLoopback(dsn); err != nil {
		t.Fatalf("pgtest: %v", err)
	}
	return dsn
}

// CheckLoopback rejects a DSN whose host is not loopback (ruling R6).
func CheckLoopback(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%s is not a postgres URL with a host", EnvDSN)
	}
	if strings.Contains(u.Host, ",") {
		return fmt.Errorf("%s names several hosts; only one loopback host is allowed", EnvDSN)
	}
	// libpq and pgx let the query string override the host, database or service
	// (also percent-encoded: ?h%6Fst=...), so only sslmode may appear there.
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return fmt.Errorf("%s has an unparsable query string: %w", EnvDSN, err)
	}
	for k := range q {
		if strings.ToLower(k) != "sslmode" {
			return fmt.Errorf("%s query parameter %q is not allowed (only sslmode)", EnvDSN, k)
		}
	}
	if !nonet.Allowed(net.JoinHostPort(u.Hostname(), "0")) {
		return fmt.Errorf("%s host %q is not loopback; tests never touch external databases", EnvDSN, u.Hostname())
	}
	return nil
}

// Reachable opens and closes a TCP connection to the DSN's host:port. It proves
// the server is listening; it does not speak the Postgres protocol.
func Reachable(ctx context.Context, dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := nonet.Dial(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return fmt.Errorf("postgres at %s:%s: %w", u.Hostname(), port, err)
	}
	return conn.Close()
}
