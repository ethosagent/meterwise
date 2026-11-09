package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/nonet"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

func TestOrgIDIsStableUUID(t *testing.T) {
	id := store.OrgID()
	if len(id) != 36 || strings.Count(id, "-") != 4 || id != store.OrgID() {
		t.Fatalf("OrgID %q is not a stable UUID", id)
	}
}

func TestOpenRejectsGarbageDSN(t *testing.T) {
	if _, err := store.Open(context.Background(), "::not a dsn::"); err == nil {
		t.Fatal("garbage DSN accepted")
	}
	if _, err := store.Migrate(context.Background(), "::not a dsn::"); err == nil {
		t.Fatal("Migrate accepted a garbage DSN")
	}
}

func TestOpenFailsFastWhenNothingListens(t *testing.T) {
	_, err := store.Open(context.Background(), "postgres://u:p@127.0.0.1:1/db?sslmode=disable")
	if err == nil {
		t.Fatal("Open succeeded against a closed port")
	}
	if strings.Contains(err.Error(), ":p@") {
		t.Fatalf("error leaks the password: %v", err)
	}
}

func TestBaselineAppliesPingsAndIsIdempotent(t *testing.T) {
	dsn := pgtest.NewDatabase(t)
	ctx := context.Background()

	n, err := store.Pending(ctx, dsn)
	if err != nil || n < 1 {
		t.Fatalf("pending before migrate = %d, %v; want every embedded migration", n, err)
	}
	applied, err := store.Migrate(ctx, dsn)
	if err != nil || len(applied) != n || applied[0].Version != 1 || !strings.HasSuffix(applied[0].Source, "0001_store.sql") {
		t.Fatalf("Migrate = %+v, %v", applied, err)
	}
	again, err := store.Migrate(ctx, dsn)
	if err != nil || len(again) != 0 {
		t.Fatalf("second Migrate applied %+v, %v; want nothing", again, err)
	}
	st, err := store.Status(ctx, dsn)
	if err != nil || len(st) != n || !st[0].Applied || !st[n-1].Applied {
		t.Fatalf("Status = %+v, %v", st, err)
	}

	s, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Queries().Ping(ctx); err != nil || ok != 1 {
		t.Fatalf("sqlc Ping = %d, %v", ok, err)
	}
	if s.Pool() == nil {
		t.Fatal("nil pool")
	}
}

// secretDSNs carry a password in the userinfo, in a percent-encoded form and in
// the query string. None of them may reach an error message.
var secretDSNs = []string{
	"postgres://app:hunter2secret@127.0.0.1:1/meterwise",
	"postgres://app:p%40ss%2Fw%3Ard9x@127.0.0.1:1/meterwise",
	"postgres://app@127.0.0.1:1/meterwise?password=qu3ryS3cret",
	"postgres://app@127.0.0.1:1/meterwise?password=%71u3ry%53ecret2",
	"postgres://app:hunter2secret@127.0.0.1:1/meterwise?sslmode=bogus",
	"postgres://app:hunter2secret@[::1/meterwise",
}

var secretFragments = []string{"hunter2secret", "p%40ss", "p@ss", "w:rd9x", "w%3Ard9x", "qu3ryS3cret", "%71u3ry", "3ecret2", "127.0.0.1", "meterwise"}

func TestErrorsCarryNoConnectionDetail(t *testing.T) {
	ctx := context.Background()
	for _, dsn := range secretDSNs {
		for name, run := range map[string]func() error{
			"open":    func() error { _, err := store.Open(ctx, dsn); return err },
			"migrate": func() error { _, err := store.Migrate(ctx, dsn); return err },
			"status":  func() error { _, err := store.Status(ctx, dsn); return err },
		} {
			err := run()
			if err == nil {
				t.Fatalf("%s accepted %q", name, dsn)
			}
			if store.ClassOf(err) == "" {
				t.Errorf("%s: error is not a store.Error: %T", name, err)
			}
			for _, frag := range secretFragments {
				if strings.Contains(err.Error(), frag) || strings.Contains(fmt.Sprintf("%+v", err), frag) {
					t.Errorf("%s: error %q leaks %q", name, err, frag)
				}
			}
			if errors.Unwrap(err) != nil {
				t.Errorf("%s: error wraps a driver error", name)
			}
		}
	}
}
