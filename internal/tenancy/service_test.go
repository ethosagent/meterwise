package tenancy_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/identity"
	"github.com/ethosagent/meterwise/internal/identity/clerkmock"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
	"github.com/ethosagent/meterwise/internal/tenancy"
)

type rig struct {
	svc  *tenancy.Service
	mock *clerkmock.Server
	clk  *clock
}

func newRig(t *testing.T, maxWS int) rig {
	t.Helper()
	r, _ := newRigLogged(t, maxWS)
	return r
}

// newRigLogged is newRig with the service's log captured.
func newRigLogged(t *testing.T, maxWS int) (rig, *syncLog) {
	t.Helper()
	logs := &syncLog{}
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(context.Background(), dsn); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	mock, err := clerkmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	clk := &clock{t: time.Now()}
	idp, err := identity.NewClerk(identity.ClerkConfig{
		Issuer: mock.Issuer(), JWKSURL: mock.JWKSURL(), SecretKey: clerkmock.BackendKey(), APIURL: mock.APIURL(), Now: clk.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := tenancy.New(db, idp, tenancy.Config{MaxWorkspaces: maxWS, Now: clk.now, Log: slog.New(slog.NewTextHandler(logs, nil))})
	if err := svc.EnsureDefaults(context.Background()); err != nil {
		t.Fatal(err)
	}
	return rig{svc: svc, mock: mock, clk: clk}, logs
}

// syncLog is a goroutine-safe log sink.
type syncLog struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *syncLog) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.b.Write(p) }
func (l *syncLog) String() string              { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

// admin makes a real admin user (invited_by references users).
func admin(t *testing.T, svc *tenancy.Service) contract.Principal {
	t.Helper()
	ctx := context.Background()
	s, err := svc.Resolve(ctx, contract.Identity{Subject: "user_admin", Email: "admin@example.test", EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureMember(ctx, s.User.ID, contract.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	s, _ = svc.Resolve(ctx, contract.Identity{Subject: "user_admin"})
	return s.Principal
}

func errCode(err error) contract.Code { return contract.AsError(err).Code }

func TestNormalizeEmailAndNames(t *testing.T) {
	if got, err := tenancy.NormalizeEmail("  Ada@Example.TEST "); err != nil || got != "ada@example.test" {
		t.Fatalf("%q %v", got, err)
	}
	for _, bad := range []string{"", "nope", "a@b@c", strings.Repeat("a", 260) + "@x.test", "Ada <ada@example.test>"} {
		if _, err := tenancy.NormalizeEmail(bad); errCode(err) != contract.CodeValidationFailed {
			t.Errorf("NormalizeEmail(%q) = %v", bad, err)
		}
	}
	r := newRig(t, 5)
	p := principal()
	for _, bad := range []string{"", "   ", strings.Repeat("x", 81), "tab\tname", "ctl\x01"} {
		if _, err := r.svc.CreateWorkspace(context.Background(), p, bad); errCode(err) != contract.CodeValidationFailed {
			t.Errorf("CreateWorkspace(%q) = %v", bad, err)
		}
	}
}

func TestSignupFlowInviteThenFirstLogin(t *testing.T) {
	r, ctx := newRig(t, 5), context.Background()
	inv, created, err := r.svc.BootstrapAdmin(ctx, "Boss@Example.test")
	if err != nil || !created || inv.Role != contract.RoleAdmin || inv.Email != "boss@example.test" || inv.InvitedBy != "" {
		t.Fatalf("bootstrap: %+v %v %v", inv, created, err)
	}
	if again, created, err := r.svc.BootstrapAdmin(ctx, "boss@example.test"); err != nil || created || again.ID != inv.ID {
		t.Fatalf("bootstrap must be idempotent: %+v %v %v", again, created, err)
	}
	if len(r.mock.Invitations()) != 1 {
		t.Fatalf("provider saw %d invitations", len(r.mock.Invitations()))
	}
	if _, _, err := r.svc.BootstrapAdmin(ctx, "bad"); errCode(err) != contract.CodeValidationFailed {
		t.Fatalf("bad email: %v", err)
	}

	stranger := contract.Identity{Subject: "user_x", Email: "x@example.test", EmailVerified: true}
	sess, err := r.svc.Resolve(ctx, stranger)
	if err != nil || sess.Member {
		t.Fatalf("a stranger resolved as member: %+v %v", sess, err)
	}
	if _, err := r.svc.CompleteSignup(ctx, sess); errCode(err) != contract.CodeNotInvited {
		t.Fatalf("stranger signup: %v", err)
	}

	boss := contract.Identity{Subject: "user_boss", Email: "BOSS@example.test", EmailVerified: false}
	sess, _ = r.svc.Resolve(ctx, boss)
	if _, err := r.svc.CompleteSignup(ctx, sess); errCode(err) != contract.CodeNotInvited {
		t.Fatalf("an unverified email must not accept an invitation: %v", err)
	}
	boss.EmailVerified = true
	sess, _ = r.svc.Resolve(ctx, boss)
	done, err := r.svc.CompleteSignup(ctx, sess)
	if err != nil || !done.Member || done.Principal.Role != contract.RoleAdmin || done.Principal.OrgID != store.OrgID() {
		t.Fatalf("signup: %+v %v", done, err)
	}
	if again, err := r.svc.CompleteSignup(ctx, done); err != nil || !again.Member {
		t.Fatalf("a member signing up again: %+v %v", again, err)
	}
	if res, _ := r.svc.Resolve(ctx, boss); !res.Member {
		t.Fatal("membership not visible on the next request")
	}
	// removal takes effect on the very next resolve
	if err := r.svc.DeleteWebhookUser(ctx, "user_boss"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Resolve(ctx, boss); errCode(err) != contract.CodeUnauthenticated {
		t.Fatalf("a deleted user resolved: %v", err)
	}
}

func TestExpiredInvitationIsNotAccepted(t *testing.T) {
	r, ctx := newRig(t, 5), context.Background()
	if _, err := r.svc.CreateInvitation(ctx, admin(t, r.svc), "late@example.test", contract.RoleMember); err != nil {
		t.Fatal(err)
	}
	r.clk.t = r.clk.t.Add(identity.InvitationLifetime + time.Hour)
	id := contract.Identity{Subject: "user_late", Email: "late@example.test", EmailVerified: true}
	sess, _ := r.svc.Resolve(ctx, id)
	if _, err := r.svc.CompleteSignup(ctx, sess); errCode(err) != contract.CodeNotInvited {
		t.Fatalf("expired: %v", err)
	}
}

func TestInvitationLifecycle(t *testing.T) {
	r, ctx := newRig(t, 5), context.Background()
	p := admin(t, r.svc)
	if _, err := r.svc.CreateInvitation(ctx, p, "x@example.test", "owner"); errCode(err) != contract.CodeValidationFailed {
		t.Fatalf("bad role: %v", err)
	}
	inv, err := r.svc.CreateInvitation(ctx, p, "m@example.test", contract.RoleMember)
	if err != nil || inv.InvitedBy != "admin@example.test" {
		t.Fatalf("create: %+v %v", inv, err)
	}
	if _, err := r.svc.CreateInvitation(ctx, p, "M@example.test", contract.RoleMember); errCode(err) != contract.CodeDuplicateInvitation {
		t.Fatalf("duplicate: %v", err)
	}
	list, err := r.svc.ListInvitations(ctx, p)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	r.mock.SetAPIDown(true)
	if _, err := r.svc.CreateInvitation(ctx, p, "down@example.test", contract.RoleMember); err == nil {
		t.Fatal("a provider outage must fail invitation creation")
	}
	if list, _ = r.svc.ListInvitations(ctx, p); len(list) != 1 {
		t.Fatal("a failed provider call left a local row")
	}
	r.mock.SetAPIDown(false)
	re, err := r.svc.ResendInvitation(ctx, p, inv.ID)
	if err != nil || re.ClerkInvitationID == inv.ClerkInvitationID {
		t.Fatalf("resend: %+v %v", re, err)
	}
	if err := r.svc.RevokeInvitation(ctx, p, inv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.ResendInvitation(ctx, p, inv.ID); errCode(err) != contract.CodeNotFound {
		t.Fatalf("resend of a revoked invitation: %v", err)
	}
	if err := r.svc.RevokeInvitation(ctx, p, inv.ID); errCode(err) != contract.CodeNotFound {
		t.Fatalf("second revoke: %v", err)
	}
	id := contract.Identity{Subject: "user_m", Email: "m@example.test", EmailVerified: true}
	sess, _ := r.svc.Resolve(ctx, id)
	if _, err := r.svc.CompleteSignup(ctx, sess); errCode(err) != contract.CodeNotInvited {
		t.Fatalf("a revoked invitation was accepted: %v", err)
	}
}

func TestInvitationsWithoutAProviderStillWork(t *testing.T) {
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(context.Background(), dsn); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	svc := tenancy.New(db, identity.Disabled{}, tenancy.Config{MaxWorkspaces: 2})
	p := admin(t, svc)
	inv, err := svc.CreateInvitation(context.Background(), p, "dev@example.test", contract.RoleMember)
	if err != nil || inv.ClerkInvitationID != "" {
		t.Fatalf("%+v %v", inv, err)
	}
	if _, err := svc.ResendInvitation(context.Background(), p, inv.ID); err != nil {
		t.Fatalf("resend without provider: %v", err)
	}
}

func TestWorkspaceLifecycleAndSwitching(t *testing.T) {
	r, ctx := newRig(t, 3), context.Background()
	if err := r.svc.EnsureDefaults(ctx); err != nil {
		t.Fatalf("EnsureDefaults must be idempotent: %v", err)
	}
	u, _ := r.svc.Resolve(ctx, contract.Identity{Subject: "user_1", Email: "a@example.test", EmailVerified: true})
	if err := r.svc.EnsureMember(ctx, u.User.ID, contract.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	sess, _ := r.svc.Resolve(ctx, contract.Identity{Subject: "user_1"})
	p := sess.Principal

	all := func(string) bool { return true }
	activeNow := func() string {
		t.Helper()
		prof, err := r.svc.Profile(ctx, u.User.ID)
		if err != nil {
			t.Fatal(err)
		}
		list, err := r.svc.ListWorkspaces(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		return tenancy.ActiveWorkspace(prof.ActiveWorkspaceID, list, all)
	}
	def := activeNow()
	if def == "" {
		t.Fatal("an unset active workspace must fall back to the default")
	}
	ws, err := r.svc.CreateWorkspace(ctx, p, "  Alpha ")
	if err != nil || ws.Name != "Alpha" || len(ws.PublicID) != 22 {
		t.Fatalf("create: %+v %v", ws, err)
	}
	if _, err := r.svc.CreateWorkspace(ctx, p, "Beta"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.CreateWorkspace(ctx, p, "Gamma"); errCode(err) != contract.CodeWorkspaceLimit {
		t.Fatalf("limit: %v", err)
	}
	list, _ := r.svc.ListWorkspaces(ctx, p)
	if len(list) != 3 {
		t.Fatalf("list: %+v", list)
	}
	if got, err := r.svc.SwitchWorkspace(ctx, p, ws.ID); err != nil || got != ws.ID {
		t.Fatalf("switch: %q %v", got, err)
	}
	if active := activeNow(); active != ws.ID {
		t.Fatalf("switch not persisted: %q", active)
	}
	if _, err := r.svc.SwitchWorkspace(ctx, p, "00000000-0000-0000-0000-000000000042"); errCode(err) != contract.CodeNotFound {
		t.Fatalf("switch to unknown: %v", err)
	}
	other := p
	other.OrgID = "00000000-0000-0000-0000-000000000042"
	if _, err := r.svc.SwitchWorkspace(ctx, other, ws.ID); errCode(err) != contract.CodeNotFound {
		t.Fatalf("a workspace outside the caller's org must be 404: %v", err)
	}
	name := "Alpha2"
	up, err := r.svc.UpdateWorkspace(ctx, p, ws.ID, contract.UpdateWorkspaceRequest{Name: &name})
	if err != nil || up.Name != name {
		t.Fatalf("update: %+v %v", up, err)
	}
	empty := " "
	if _, err := r.svc.UpdateWorkspace(ctx, p, ws.ID, contract.UpdateWorkspaceRequest{Name: &empty}); errCode(err) != contract.CodeValidationFailed {
		t.Fatalf("blank rename: %v", err)
	}
	if err := r.svc.DeleteWorkspace(ctx, p, def); errCode(err) != contract.CodeDefaultWorkspace {
		t.Fatalf("delete default: %v", err)
	}
	if err := r.svc.DeleteWorkspace(ctx, p, ws.ID); err != nil {
		t.Fatal(err)
	}
	if active := activeNow(); active != def {
		t.Fatalf("a deleted active workspace must fall back to the default, got %q", active)
	}
	if err := r.svc.DeleteWorkspace(ctx, p, ws.ID); errCode(err) != contract.CodeNotFound {
		t.Fatalf("second delete: %v", err)
	}
}

func TestPreferencesRoundTrip(t *testing.T) {
	r, ctx := newRig(t, 2), context.Background()
	s, _ := r.svc.Resolve(ctx, contract.Identity{Subject: "user_p"})
	dark, on := contract.ThemeDark, true
	got, err := r.svc.UpdatePreferences(ctx, s.User.ID, contract.PreferencesPatch{Theme: &dark, ReducedMotion: &on})
	if err != nil || got.Theme != dark || !got.ReducedMotion {
		t.Fatalf("%+v %v", got, err)
	}
	if prof, _ := r.svc.Profile(ctx, s.User.ID); prof.Preferences.Theme != dark {
		t.Fatalf("%+v", prof)
	}
}

func TestWebhookUserApply(t *testing.T) {
	r, ctx := newRig(t, 2), context.Background()
	err := r.svc.ApplyWebhookUser(ctx, identity.WebhookUser{ClerkUserID: "user_w", Email: "W@example.test", EmailVerified: true, Name: "Wes"})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := r.svc.Resolve(ctx, contract.Identity{Subject: "user_w"})
	if s.User.Email != "w@example.test" || s.User.Name != "Wes" {
		t.Fatalf("%+v", s.User)
	}
}
