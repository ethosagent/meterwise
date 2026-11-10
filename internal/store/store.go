package store

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// orgID is the one seeded organization (decision D4). Plan 02 inserts the row
// with this id; every table keeps org_id so multi-org is additive.
const orgID = "00000000-0000-0000-0000-000000000001"

// OrgID returns the id of the single organization.
func OrgID() string { return orgID }

// connectTimeout bounds one dial; it is set in code, never in the DSN (ruling R20).
const connectTimeout = 5 * time.Second

// Store is the database handle shared by the control plane and, later, the ledger.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries

	log         atomic.Pointer[slog.Logger]
	badTools    badData // unreadable calls.tool_calls values seen by Session
	badEvidence badData // unreadable monitor_events.evidence values seen by the event reads
}

// Open parses dsn, opens a pool and pings it, so a bad URL or a down database
// fails at boot instead of on the first request.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fail(ClassBadURL, "store: DATABASE_URL is not a valid postgres URL")
	}
	cfg.ConnConfig.ConnectTimeout = connectTimeout
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fail(ClassPool, "store: cannot open the connection pool")
	}
	s := &Store{pool: pool, q: sqlc.New(pool)}
	if err := s.Ping(ctx); err != nil {
		pool.Close()
		return nil, fail(ClassUnreachable, "store: database unreachable")
	}
	return s, nil
}

// Ping proves the database answers a query (the sqlc Ping query).
func (s *Store) Ping(ctx context.Context) error {
	if _, err := s.q.Ping(ctx); err != nil {
		return fail(ClassUnreachable, "store: database unreachable")
	}
	return nil
}

// Pool exposes the pgx pool to the packages that own tables (Plan 02 onward).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Queries returns the generated queries bound to the pool.
func (s *Store) Queries() *sqlc.Queries { return s.q }

// Close releases every connection.
func (s *Store) Close() { s.pool.Close() }
