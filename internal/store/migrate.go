package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migration is one migration and whether it is applied.
type Migration struct {
	Version int64  `json:"version"`
	Source  string `json:"source"`
	Applied bool   `json:"applied"`
}

func provider(db *sql.DB) (*goose.Provider, error) {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("store: migrations: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, sub)
	if err != nil {
		return nil, fmt.Errorf("store: goose: %w", err)
	}
	return p, nil
}

// withDB runs fn on a database/sql handle (goose needs one) backed by a short-lived pool.
func withDB(ctx context.Context, dsn string, fn func(*goose.Provider) error) error {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("store: DATABASE_URL is not a valid postgres URL")
	}
	cfg.ConnConfig.ConnectTimeout = connectTimeout
	db := stdlib.OpenDB(*cfg.ConnConfig)
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("store: database unreachable: %w", err)
	}
	p, err := provider(db)
	if err != nil {
		return err
	}
	return fn(p)
}

// Migrate applies every pending migration and returns the ones it applied.
func Migrate(ctx context.Context, dsn string) ([]Migration, error) {
	var out []Migration
	err := withDB(ctx, dsn, func(p *goose.Provider) error {
		res, err := p.Up(ctx)
		for _, r := range res {
			out = append(out, Migration{Version: r.Source.Version, Source: r.Source.Path, Applied: r.Error == nil})
		}
		return err
	})
	return out, err
}

// Status lists every embedded migration with its applied state.
func Status(ctx context.Context, dsn string) ([]Migration, error) {
	var out []Migration
	err := withDB(ctx, dsn, func(p *goose.Provider) error {
		st, err := p.Status(ctx)
		for _, s := range st {
			out = append(out, Migration{Version: s.Source.Version, Source: s.Source.Path, Applied: s.State == goose.StateApplied})
		}
		return err
	})
	return out, err
}

// Pending reports how many embedded migrations are not applied yet.
func Pending(ctx context.Context, dsn string) (int, error) {
	st, err := Status(ctx, dsn)
	n := 0
	for _, m := range st {
		if !m.Applied {
			n++
		}
	}
	return n, err
}
