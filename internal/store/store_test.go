package store_test

import (
	"context"
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
	if err != nil || n != 1 {
		t.Fatalf("pending before migrate = %d, %v; want 1", n, err)
	}
	applied, err := store.Migrate(ctx, dsn)
	if err != nil || len(applied) != 1 || applied[0].Version != 1 || !strings.HasSuffix(applied[0].Source, "0001_store.sql") {
		t.Fatalf("Migrate = %+v, %v", applied, err)
	}
	again, err := store.Migrate(ctx, dsn)
	if err != nil || len(again) != 0 {
		t.Fatalf("second Migrate applied %+v, %v; want nothing", again, err)
	}
	st, err := store.Status(ctx, dsn)
	if err != nil || len(st) != 1 || !st[0].Applied {
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
