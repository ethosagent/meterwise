package tenancy_test

import (
	"context"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// The Clerk user.deleted webhook can remove the last active admin (accepted: the API paths guard
// last_admin, the provider's own deletion cannot be refused). That is acceptable only because the
// operator can recover: bootstrap-admin must hand a fresh admin invitation to the email, and the
// person who signs up again with it (a new Clerk user) becomes an active admin with no groups.
func TestBootstrapAdminRecoversAfterTheWebhookDeletesTheLastAdmin(t *testing.T) {
	r, ctx := newRig(t, 5), context.Background()
	old := contract.Identity{Subject: "user_boss", Email: "boss@example.test", EmailVerified: true}
	if _, _, err := r.svc.BootstrapAdmin(ctx, "boss@example.test"); err != nil {
		t.Fatal(err)
	}
	sess, _ := r.svc.Resolve(ctx, old)
	if _, err := r.svc.CompleteSignup(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.DeleteWebhookUser(ctx, "user_boss"); err != nil {
		t.Fatal(err)
	}
	if n, err := r.db.ActiveAdminCount(ctx, store.OrgID()); err != nil || n != 0 {
		t.Fatalf("setup: %d active admins, %v", n, err)
	}

	inv, created, err := r.svc.BootstrapAdmin(ctx, "boss@example.test")
	if err != nil || !created || inv.Role != contract.RoleAdmin {
		t.Fatalf("recovery: %+v created=%v err=%v", inv, created, err)
	}
	if _, again, err := r.svc.BootstrapAdmin(ctx, "boss@example.test"); err != nil || again {
		t.Fatalf("recovery is idempotent while the invitation is open: created=%v err=%v", again, err)
	}

	renewed := contract.Identity{Subject: "user_boss_again", Email: "boss@example.test", EmailVerified: true}
	sess, _ = r.svc.Resolve(ctx, renewed)
	if _, err := r.svc.CompleteSignup(ctx, sess); err != nil {
		t.Fatalf("the re-registered admin could not claim the invitation: %v", err)
	}
	got, _ := r.svc.Resolve(ctx, renewed)
	if !got.Member || got.Principal.Role != contract.RoleAdmin {
		t.Fatalf("not an active admin again: %+v", got)
	}
}

// Recovery must not become a back door: while an admin is active, a deleted admin's email gets the
// ordinary conflict, not a fresh admin invitation.
func TestBootstrapAdminAfterADeletedAdminNeedsNoActiveAdmin(t *testing.T) {
	r, ctx := newRig(t, 5), context.Background()
	adm := admin(t, r.svc) // an active admin who stays
	if _, err := r.svc.CreateInvitation(ctx, adm, "other@example.test", contract.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	id := contract.Identity{Subject: "user_other", Email: "other@example.test", EmailVerified: true}
	sess, _ := r.svc.Resolve(ctx, id)
	if _, err := r.svc.CompleteSignup(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.DeleteWebhookUser(ctx, "user_other"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.svc.BootstrapAdmin(ctx, "other@example.test"); errCode(err) != contract.CodeConflict {
		t.Fatalf("with an active admin the conflict stands: %v", err)
	}
}
