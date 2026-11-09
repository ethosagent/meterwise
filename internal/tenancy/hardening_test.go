package tenancy_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/tenancy"
)

// Non-ASCII characters that Unicode case folding maps onto ASCII.
var foldingImpostors = map[string]string{
	"kelvin sign (U+212A) for k":        "\u212aate@corp.test",
	"dotted capital I (U+0130) for i":   "m\u0130ke@corp.test",
	"kelvin sign in the domain":         "ceo@\u212apmg.test",
	"dotted capital I in the domain":    "ceo@\u0130bm.test",
	"long s (U+017F) for s":             "\u017fam@corp.test",
	"non-ASCII with surrounding spaces": "  \u212aate@corp.test ",
	// Unicode whitespace is not trimmed away before the ASCII check: the raw value is judged.
	"no-break space before the address":  "\u00a0kate@corp.test",
	"no-break space after the address":   "kate@corp.test\u00a0",
	"ideographic space around":           "\u3000kate@corp.test\u3000",
	"em space before the address":        "\u2003kate@corp.test",
	"zero width no-break space (U+FEFF)": "\ufeffkate@corp.test",
}

func TestNormalizeEmailRefusesNonASCIIBeforeFolding(t *testing.T) {
	for name, raw := range foldingImpostors {
		t.Run(name, func(t *testing.T) {
			if got, err := tenancy.NormalizeEmail(raw); errCode(err) != contract.CodeValidationFailed {
				t.Fatalf("NormalizeEmail(%q) = %q, %v", raw, got, err)
			}
		})
	}
	// the ToLower line itself still folds ASCII
	if got, err := tenancy.NormalizeEmail("KATE@Corp.TEST"); err != nil || got != "kate@corp.test" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestAFoldingImpostorCannotInviteOrClaimAnInvitation(t *testing.T) {
	r, ctx := newRig(t, 5), context.Background()
	adm := admin(t, r.svc)
	if _, err := r.svc.CreateInvitation(ctx, adm, "kate@corp.test", contract.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.CreateInvitation(ctx, adm, "mike@corp.test", contract.RoleMember); err != nil {
		t.Fatal(err)
	}
	// invite path: a non-ASCII address is refused, so it cannot even create a row that matches
	for name, raw := range foldingImpostors {
		if _, err := r.svc.CreateInvitation(ctx, adm, raw, contract.RoleMember); errCode(err) != contract.CodeValidationFailed {
			t.Errorf("invite %s: %v", name, err)
		}
	}
	// token path: a verified token whose email folds onto the invited address gets nothing
	for i, raw := range []string{"\u212aate@corp.test", "m\u0130ke@corp.test"} {
		id := contract.Identity{Subject: "user_impostor_" + string(rune('a'+i)), Email: raw, EmailVerified: true}
		sess, err := r.svc.Resolve(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.svc.CompleteSignup(ctx, sess); errCode(err) != contract.CodeNotInvited {
			t.Fatalf("%q claimed an invitation: %v", raw, err)
		}
		if again, _ := r.svc.Resolve(ctx, id); again.Member {
			t.Fatalf("%q became a member", raw)
		}
	}
	// both invitations are still open for their real owners
	list, _ := r.svc.ListInvitations(ctx, adm)
	open := 0
	for _, i := range list {
		if i.StatusAt(r.clk.now()) == contract.InvitationPending && (i.Email == "kate@corp.test" || i.Email == "mike@corp.test") {
			open++
		}
	}
	if open != 2 {
		t.Fatalf("%d of the 2 invitations are still pending", open)
	}
	owner := contract.Identity{Subject: "user_kate", Email: "Kate@Corp.test", EmailVerified: true}
	sess, _ := r.svc.Resolve(ctx, owner)
	if done, err := r.svc.CompleteSignup(ctx, sess); err != nil || done.Principal.Role != contract.RoleAdmin {
		t.Fatalf("the real owner: %+v %v", done, err)
	}
}

func TestTheSameUserFinishingFirstLoginTwiceAtOnceIsNeverRefused(t *testing.T) {
	r, ctx := newRig(t, 5), context.Background()
	if _, _, err := r.svc.BootstrapAdmin(ctx, "boss@example.test"); err != nil {
		t.Fatal(err)
	}
	id := contract.Identity{Subject: "user_boss", Email: "boss@example.test", EmailVerified: true}
	const tabs = 12
	var wg sync.WaitGroup
	errs := make(chan error, tabs)
	for range tabs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess, err := r.svc.Resolve(ctx, id)
			if err == nil {
				_, err = r.svc.CompleteSignup(ctx, sess)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a tab was refused although the person became a member: %v", err)
		}
	}
	// a different person with no invitation is still refused
	other, _ := r.svc.Resolve(ctx, contract.Identity{Subject: "user_other", Email: "other@example.test", EmailVerified: true})
	if _, err := r.svc.CompleteSignup(ctx, other); errCode(err) != contract.CodeNotInvited {
		t.Fatalf("%v", err)
	}
}

func TestBootstrapAdminMisuse(t *testing.T) {
	r, ctx := newRig(t, 5), context.Background()
	adm := admin(t, r.svc)

	// an open member invitation is not silently reported as an admin one
	if _, err := r.svc.CreateInvitation(ctx, adm, "dev@example.test", contract.RoleMember); err != nil {
		t.Fatal(err)
	}
	if inv, created, err := r.svc.BootstrapAdmin(ctx, "dev@example.test"); errCode(err) != contract.CodeConflict || created || inv.ID != "" {
		t.Fatalf("member invitation: %+v %v %v", inv, created, err)
	}

	// an email that already accepted an invitation does not get a second, open admin invitation
	if _, _, err := r.svc.BootstrapAdmin(ctx, "boss@example.test"); err != nil {
		t.Fatal(err)
	}
	sess, _ := r.svc.Resolve(ctx, contract.Identity{Subject: "user_boss", Email: "boss@example.test", EmailVerified: true})
	if _, err := r.svc.CompleteSignup(ctx, sess); err != nil {
		t.Fatal(err)
	}
	before, _ := r.svc.ListInvitations(ctx, adm)
	if _, created, err := r.svc.BootstrapAdmin(ctx, "BOSS@example.test"); errCode(err) != contract.CodeConflict || created {
		t.Fatalf("accepted admin: %v %v", created, err)
	}
	if after, _ := r.svc.ListInvitations(ctx, adm); len(after) != len(before) {
		t.Fatalf("a refused bootstrap still created %d invitation(s)", len(after)-len(before))
	}

	// the idempotent case is unchanged
	first, created, err := r.svc.BootstrapAdmin(ctx, "new@example.test")
	again, createdAgain, err2 := r.svc.BootstrapAdmin(ctx, "New@Example.test")
	if err != nil || err2 != nil || !created || createdAgain || first.ID != again.ID {
		t.Fatalf("idempotent: %v %v %v %v", created, createdAgain, err, err2)
	}
}

func TestSwallowedProviderCompensationsAreLoggedAtWarn(t *testing.T) {
	r, logs := newRigLogged(t, 5)
	ctx := context.Background()
	adm := admin(t, r.svc)
	inv, err := r.svc.CreateInvitation(ctx, adm, "left@example.test", contract.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	r.mock.SetAPIDown(true)
	if err := r.svc.RevokeInvitation(ctx, adm, inv.ID); err != nil {
		t.Fatalf("revoke must still succeed in the database: %v", err)
	}
	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "provider invitation cleanup failed") || !strings.Contains(out, "provider_invitation_id=") {
		t.Fatalf("the swallowed provider failure was not logged: %q", out)
	}
	if strings.Contains(out, "left@example.test") {
		t.Fatalf("the log carries the email: %q", out)
	}
}

func TestResendThatFailsToRefreshRevokesTheNewProviderInvitation(t *testing.T) {
	r, logs := newRigLogged(t, 5)
	ctx := context.Background()
	adm := admin(t, r.svc)
	inv, err := r.svc.CreateInvitation(ctx, adm, "again@example.test", contract.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	// Revoke the row between the resend's read and its refresh by using a stale id: the refresh updates
	// no row once the invitation is revoked, which is the failure under test.
	revoked := make(chan struct{})
	r.mock.OnCreateInvitation(func() {
		_ = r.svc.RevokeInvitation(ctx, adm, inv.ID)
		close(revoked)
	})
	if _, err := r.svc.ResendInvitation(ctx, adm, inv.ID); err == nil {
		t.Fatal("the resend of a concurrently revoked invitation succeeded")
	}
	<-revoked
	for _, pi := range r.mock.Invitations() {
		if !pi.Revoked {
			t.Fatalf("provider invitation %s was left active (orphan)", pi.ID)
		}
	}
	_ = logs
}

// A failed re-read of the membership is our failure, never a verdict on the person.
func TestCompleteSignupSurfacesAFailedMembershipReread(t *testing.T) {
	r, ctx := newRig(t, 5), context.Background()
	sess, err := r.svc.Resolve(ctx, contract.Identity{Subject: "user_nobody", Email: "nobody@example.test", EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Pool().Exec(ctx, "ALTER TABLE memberships RENAME TO memberships_gone"); err != nil {
		t.Fatal(err)
	}
	_, err = r.svc.CompleteSignup(ctx, sess)
	if err == nil {
		t.Fatal("CompleteSignup succeeded without a readable membership table")
	}
	if errCode(err) == contract.CodeNotInvited {
		t.Fatalf("a store failure was reported as not_invited: %v", err)
	}
	if got := contract.AsError(err).Status(); got != 500 && got != 503 {
		t.Fatalf("status %d for %v, want 500 or 503", got, err)
	}
}

// A removed admin may be the only way back in: bootstrap-admin re-activates that admin.
func TestBootstrapAdminReactivatesARemovedAdmin(t *testing.T) {
	r, ctx := newRig(t, 5), context.Background()
	id := contract.Identity{Subject: "user_boss", Email: "boss@example.test", EmailVerified: true}
	if _, _, err := r.svc.BootstrapAdmin(ctx, "boss@example.test"); err != nil {
		t.Fatal(err)
	}
	sess, _ := r.svc.Resolve(ctx, id)
	if _, err := r.svc.CompleteSignup(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.svc.BootstrapAdmin(ctx, "boss@example.test"); errCode(err) != contract.CodeConflict {
		t.Fatalf("an active admin keeps the conflict: %v", err)
	}
	if err := r.db.SetMembershipStatus(ctx, store.OrgID(), sess.User.ID, "removed"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.svc.Resolve(ctx, id); got.Member {
		t.Fatal("setup: the admin is still a member")
	}
	inv, created, err := r.svc.BootstrapAdmin(ctx, "Boss@Example.test")
	if err != nil || created || inv.Email != "boss@example.test" {
		t.Fatalf("re-activation: %+v created=%v err=%v", inv, created, err)
	}
	if got, _ := r.svc.Resolve(ctx, id); !got.Member || got.Principal.Role != contract.RoleAdmin {
		t.Fatalf("not an active admin again: %+v", got)
	}
}

// A removed plain member is not a way back in for bootstrap-admin: Settings re-invites.
func TestBootstrapAdminDoesNotReactivateARemovedMember(t *testing.T) {
	r, ctx := newRig(t, 5), context.Background()
	adm := admin(t, r.svc)
	if _, err := r.svc.CreateInvitation(ctx, adm, "dev@example.test", contract.RoleMember); err != nil {
		t.Fatal(err)
	}
	sess, _ := r.svc.Resolve(ctx, contract.Identity{Subject: "user_dev", Email: "dev@example.test", EmailVerified: true})
	if _, err := r.svc.CompleteSignup(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if err := r.db.SetMembershipStatus(ctx, store.OrgID(), sess.User.ID, "removed"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.svc.BootstrapAdmin(ctx, "dev@example.test"); errCode(err) != contract.CodeConflict {
		t.Fatalf("%v", err)
	}
	if got, _ := r.svc.Resolve(ctx, contract.Identity{Subject: "user_dev"}); got.Member {
		t.Fatal("a removed member became active through bootstrap-admin")
	}
}
