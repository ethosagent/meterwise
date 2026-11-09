package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// Every path that can end an admin membership waits on the admin lock: a demotion, a removal and
// the Clerk user.deleted webhook. Each case holds the lock in another transaction and expects the
// call to block until it is released.
func TestEveryAdminEndingPathTakesTheAdminLock(t *testing.T) {
	org := store.OrgID()
	paths := map[string]func(ctx context.Context, s *store.Store, userID string) error{
		"SetMemberRole": func(ctx context.Context, s *store.Store, id string) error {
			_, err := s.SetMemberRole(ctx, org, id, contract.RoleMember)
			return err
		},
		"RemoveMember": func(ctx context.Context, s *store.Store, id string) error {
			_, err := s.RemoveMember(ctx, org, id, time.Now())
			return err
		},
		"SoftDeleteUser": func(ctx context.Context, s *store.Store, _ string) error {
			return s.SoftDeleteUser(ctx, org, "user_second_admin")
		},
	}
	for name, call := range paths {
		t.Run(name, func(t *testing.T) { holdAdminLockAndCall(t, name, call) })
	}
}

func holdAdminLockAndCall(t *testing.T, name string, call func(ctx context.Context, s *store.Store, userID string) error) {
	t.Helper()
	ctx, org := context.Background(), store.OrgID()
	s, conn := auditRig(t)
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
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('admins:' || $1::text))`, org); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- call(ctx, s, ids[1]) }()
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
}

// The already_member check is part of the insert transaction: a member that exists when the row is
// written blocks it, whatever the caller checked a moment earlier.
func TestCreateInvitationRefusesAnActiveMemberInsideItsTransaction(t *testing.T) {
	s, ctx, org := openMigrated(t), context.Background(), store.OrgID()
	u, err := s.UpsertUser(ctx, org, contract.Identity{Subject: "user_m", Email: "m@corp.test", EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertActiveMembership(ctx, org, u.ID, contract.RoleMember); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_, err = s.CreateInvitation(ctx, store.NewInvitation{OrgID: org, Email: "m@corp.test", Role: contract.RoleMember, ExpiresAt: now.Add(time.Hour), Now: now})
	if code(err) != contract.CodeAlreadyMember {
		t.Fatalf("got %v, want already_member", err)
	}
	if list, _ := s.ListInvitations(ctx, org); len(list) != 0 {
		t.Fatalf("a refused invitation left %d row(s)", len(list))
	}
}
