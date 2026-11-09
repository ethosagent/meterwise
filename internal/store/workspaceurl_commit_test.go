package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ethosagent/meterwise/internal/store/pgtest"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// afterCommit counts the statements that start once a COMMIT has finished. A reset must run none:
// anything it did then (a re-read) could fail on a dropped connection or cancelled request and
// report an error for a change that already happened.
type afterCommit struct{ committed, after *int }

func (c afterCommit) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	if *c.committed > 0 {
		*c.after++
	}
	return ctx
}

func (c afterCommit) TraceQueryEnd(_ context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	if strings.EqualFold(d.CommandTag.String(), "COMMIT") {
		*c.committed++
	}
}

func TestResetRunsNothingAfterItsCommit(t *testing.T) {
	dsn := pgtest.NewDatabase(t)
	if _, err := Migrate(context.Background(), dsn); err != nil {
		t.Fatal(err)
	}
	var committed, after int
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Tracer = afterCommit{&committed, &after}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s := &Store{pool: pool, q: sqlc.New(pool)}

	ws, err := s.CreateWorkspace(context.Background(), OrgID(), "Alpha", "AAAAAAAAAAAAAAAAAAAAAA", 9)
	if err != nil {
		t.Fatal(err)
	}
	committed, after = 0, 0
	got, _, err := s.ResetWorkspaceURL(context.Background(), OrgID(), ws.ID, "BBBBBBBBBBBBBBBBBBBBBB", time.Minute, time.Now())
	if err != nil || got.PublicID != "BBBBBBBBBBBBBBBBBBBBBB" {
		t.Fatalf("a committed reset must be reported as done: %+v %v", got, err)
	}
	if committed != 1 || after != 0 {
		t.Fatalf("commits=%d statements after the commit=%d, want 1 and 0", committed, after)
	}
}
