package pgtest_test

import (
	"context"
	"net"
	"os"
	"testing"

	"github.com/ethosagent/meterwise/internal/nonet"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

func TestCheckLoopback(t *testing.T) {
	tests := map[string]bool{
		"postgres://u:p@127.0.0.1:54329/db":                       true,
		"postgres://u:p@localhost:5432/db":                        true,
		"postgres://u:p@[::1]:5432/db":                            true,
		"postgres://u:p@db.example.com/db":                        false,
		"postgres://u:p@10.1.2.3:5432/db":                         false,
		"not a url":                                               false,
		"postgres://u:p@127.0.0.1/db?sslmode=disable":             true,
		"postgres://u:p@127.0.0.1/db?host=db.example.com":         false,
		"postgres://u:p@127.0.0.1/db?HOSTADDR=93.184.216.34":      false,
		"postgres://u:p@127.0.0.1/db?h%6Fst=db.example.com":       false,
		"postgres://u:p@127.0.0.1/db?dbname=real":                 false,
		"postgres://u:p@127.0.0.1/db?service=prod":                false,
		"postgres://u:p@127.0.0.1/db?sslmode=disable&host=remote": false,
		"postgres://u:p@127.0.0.1/db?%zz=1":                       false,
		"postgres://127.0.0.1:1,db.example.com:2/db":              false,
	}
	for dsn, ok := range tests {
		if err := pgtest.CheckLoopback(dsn); (err == nil) != ok {
			t.Errorf("CheckLoopback(%q) err=%v, want ok=%v", dsn, err, ok)
		}
	}
}

func TestReachableAgainstALoopbackListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dsn := "postgres://u:p@" + ln.Addr().String() + "/db"
	if err := pgtest.Reachable(context.Background(), dsn); err != nil {
		t.Fatalf("listener is up but Reachable failed: %v", err)
	}
	_ = ln.Close()
	if err := pgtest.Reachable(context.Background(), dsn); err == nil {
		t.Fatal("closed listener reported reachable")
	}
}

// TestConfiguredDatabaseIsReachable is the live seam check: with
// TEST_DATABASE_URL set (make db-up) the compose Postgres must be listening.
// Without it, DSN skips loudly locally and fails under CI=true.
func TestConfiguredDatabaseIsReachable(t *testing.T) {
	dsn := pgtest.DSN(t)
	if err := pgtest.Reachable(context.Background(), dsn); err != nil {
		t.Fatal(err)
	}
}
