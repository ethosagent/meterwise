package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

// The group branch of ListGrantsForUser must scope the permission side by org too: a
// group_permissions or resource_permissions row of another org is never a grant, even if it links
// to a group of this org. The schema allows one org (organizations_single), so the test drops that
// index in its own throwaway database to be able to build the foreign rows.
func TestGrantsIgnoreForeignOrgPermissionRows(t *testing.T) {
	ctx, org := context.Background(), store.OrgID()
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	u, err := s.UpsertUser(ctx, org, contract.Identity{Subject: "user_1", Email: "a@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.CreateGroup(ctx, org, "Readers")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	const foreign = "00000000-0000-0000-0000-0000000000f0"
	for _, q := range []string{
		`DROP INDEX organizations_single`,
		`INSERT INTO organizations (id, name) VALUES ('` + foreign + `', 'Foreign')`,
		`INSERT INTO group_users (group_id, user_id, org_id) VALUES ('` + g.ID + `', '` + u.ID + `', '` + org + `')`,
	} {
		if _, err := conn.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	// A foreign-org catalog row linked, by a foreign-org link row, to this org's group.
	if _, err := conn.Exec(ctx, `
WITH rp AS (INSERT INTO resource_permissions (org_id, resource_type, action) VALUES ('`+foreign+`', 'member', 'manage') RETURNING id)
INSERT INTO group_permissions (group_id, permission_id, org_id) SELECT '`+g.ID+`', id, '`+foreign+`' FROM rp`); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Grants(ctx, org, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.GroupID != "" {
			t.Fatalf("a foreign-org permission row was read as a grant: %+v", r)
		}
	}
}
