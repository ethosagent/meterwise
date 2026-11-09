// Package store owns the Postgres connection: the pgx pool, the goose
// migrations embedded from migrations/, the sqlc queries in sqlc/ and the
// single organization's id (OrgID). It imports the standard library, pgx and
// goose only; feature packages above it receive a *Store, never a DSN.
package store
