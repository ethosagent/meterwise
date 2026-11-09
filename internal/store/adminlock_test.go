package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

// Every path that can end an admin membership waits on the admin lock: a demotion, a removal and
// the Clerk user.deleted webhook. Each case holds the lock in another transaction and expects the
// call to block until it is released.
func TestEveryAdminEndingPathTakesTheAdminLock(t *testing.T) {
	ctx, org := context.Background(), store.OrgID()
	paths := map[string]func(s *store.Store, userID string) error{
		"SetMemberRole": func(s *store.Store, id string) error {
			_, err := s.SetMemberRole(ctx, org, id, contract.RoleMember)
			return err
		},
		"RemoveMember": func(s *store.Store, id string) error {
			_, err := s.RemoveMember(ctx, org, id, time.Now())
			return err
		},
		"SoftDeleteUser": func(s *store.Store, _ string) error { return s.SoftDeleteUser(ctx, org, "user_second_admin") },
	}
	for name, call := range paths {
		t.Run(name, func(t *testing.T) {
			dsn := pgtest.NewDatabase(t)
			if _, err := store.Migrate(ctx, dsn); err != nil {
				t.Fatal(err)
			}
			s, err := store.Open(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.Close)
			var ids []string
			for _, sub := range []string{"user_first_admin", "user_second_admin"} {
				u, err := s.UpsertUser(ctx, org, contract.Identity{Subject: sub, Email: sub + "@corp.test", EmailVerified: true})
				if err != nil {
					t.Fatal(err)
				}
				if err := s.UpsertActiveMembership(ctx, org, u.ID, contract.RoleAdmin); err != nil {
					t.Fatal(err)
				}
				ids = append(ids, u.ID)
			}
			conn, err := pgx.Connect(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close(ctx) })
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('admins:' || $1::text))`, org); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- call(s, ids[1]) }()
			select {
			case err := <-done:
				t.Fatalf("%s finished (%v) while the admin lock was held elsewhere", name, err)
			case <-time.After(400 * time.Millisecond):
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("%s after release: %v", name, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("%s did not finish after the lock was released", name)
			}
		})
	}
}
