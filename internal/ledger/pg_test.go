package ledger_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/ledger"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

func pgStore(t testing.TB) (*store.Store, string) {
	t.Helper()
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(context.Background(), dsn); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	ws, err := s.CreateWorkspace(context.Background(), store.OrgID(), "Demo", "AAAAAAAAAAAAAAAAAAAAAA", 5)
	if err != nil {
		t.Fatal(err)
	}
	return s, ws.ID
}

func scalar[T any](t testing.TB, s *store.Store, q string, args ...any) T {
	t.Helper()
	var v T
	if err := s.Pool().QueryRow(context.Background(), q, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return v
}

func TestRealPostgresOneRowPerCallSessionAggregatedAndClosed(t *testing.T) {
	s, ws := pgStore(t)
	h := newHook(t, s, nil)
	fx := []string{"openai/chat-stream-tools", "openai/chat-stream-text", "anthropic/messages-stream-tool-use", "openai/chat", "openai/chat-stream-text"}
	driveAs(t, h, tenantAuth{ws: ws}, fx...)
	ctx := context.Background()
	if n := scalar[int](t, s, `SELECT count(*) FROM calls WHERE workspace_id = $1`, ws); n != len(fx) {
		t.Fatalf("calls rows = %d, want %d", n, len(fx))
	}
	sess, calls, _, err := s.Session(ctx, ws, "testkit-session")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Calls != 5 || sess.Outcome != contract.OutcomeCompleted || sess.ClosedAt == nil || sess.Cost == nil || sess.Source != contract.SessionSourceHeader {
		t.Fatalf("session = %+v", sess)
	}
	var in, out int64
	var cost contract.MicroUSD
	for _, c := range calls {
		in, out = in+c.Usage.Input, out+c.Usage.Output
		if c.Cost != nil {
			cost += *c.Cost
		}
	}
	if sess.InputTokens != in || sess.OutputTokens != out || *sess.Cost != cost || cost <= 0 {
		t.Fatalf("session sums %d/%d/%d != call sums %d/%d/%d", sess.InputTokens, sess.OutputTokens, *sess.Cost, in, out, cost)
	}
	if len(calls[0].ToolCalls) != 1 || calls[0].ToolCalls[0] != "get_weather" || calls[0].Wire != "openai.chat" {
		t.Fatalf("first call = %+v", calls[0])
	}
	if scalar[time.Time](t, s, `SELECT last_call_at FROM workspaces WHERE id = $1`, ws).IsZero() {
		t.Fatal("workspaces.last_call_at not touched")
	}
	// a second run of the same fixtures in a fresh hook is a new set of calls: ids are unique per call
	h2 := newHook(t, s, nil)
	driveAs(t, h2, tenantAuth{ws: ws}, "openai/chat-stream-text")
	if sess2, _, _, _ := s.Session(ctx, ws, "testkit-session"); sess2.Calls != 6 {
		t.Fatalf("session after a second call = %+v", sess2)
	}
}

func allText(t testing.TB, s *store.Store) string {
	t.Helper()
	ctx := context.Background()
	var b strings.Builder
	for _, q := range []string{
		`SELECT coalesce(string_agg(to_jsonb(c)::text, ' '), '') FROM calls c`,
		`SELECT coalesce(string_agg(to_jsonb(x)::text, ' '), '') FROM sessions x`,
		`SELECT coalesce(string_agg(to_jsonb(m)::text, ' '), '') FROM monitor_events m`,
		`SELECT coalesce(string_agg(to_jsonb(a)::text, ' '), '') FROM audit_log a`,
		`SELECT coalesce(string_agg(to_jsonb(w)::text, ' '), '') FROM workspaces w`,
	} {
		var part string
		if err := s.Pool().QueryRow(ctx, q).Scan(&part); err != nil {
			t.Fatal(err)
		}
		b.WriteString(part)
	}
	return b.String()
}

func TestNoMessageTextIsPersistedUnlessCaptureOnAndPrivacyOff(t *testing.T) {
	for _, tc := range []struct {
		name        string
		policy      *tollgate.Policy
		global      bool
		wantPreview bool
	}{
		{"default: privacy mode, no capture", nil, true, false},
		{"capture requested but global privacy on", &tollgate.Policy{Capture: true}, true, false},
		{"global privacy off but the workspace did not opt in", &tollgate.Policy{}, false, false},
		{"capture on and privacy off", &tollgate.Policy{Capture: true}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, ws := pgStore(t)
			h := newHook(t, s, func(o *ledger.Options) { o.PrivacyMode = &tc.global })
			c := directCall("priv-1", userSentinel, tc.policy, 1)
			c.Tenant.ID = ws
			c.Session.TenantID = ws
			complete(t, h, c)
			c2 := directCall("priv-2", userSentinel+"-second", tc.policy, 2)
			c2.Tenant.ID = ws
			complete(t, h, c2)
			if err := h.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if scalar[int](t, s, `SELECT count(*) FROM calls`) != 2 {
				t.Fatal("calls not written")
			}
			text := allText(t, s)
			for _, secret := range []string{systemSentinel, keySentinel, "Bearer"} {
				if strings.Contains(text, secret) {
					t.Errorf("%q persisted in the ledger", secret)
				}
			}
			preview := scalar[*string](t, s, `SELECT first_user_message_preview FROM sessions`)
			if tc.wantPreview != (preview != nil) {
				t.Fatalf("preview = %v, want present=%v", preview, tc.wantPreview)
			}
			if !tc.wantPreview && strings.Contains(text, userSentinel) {
				t.Fatal("user message text persisted although capture is off or privacy mode is on")
			}
			if tc.wantPreview {
				if *preview != userSentinel {
					t.Errorf("preview = %q", *preview)
				}
				if strings.Contains(text, userSentinel+"-second") {
					t.Error("only the session's first message may be kept")
				}
				if n := scalar[int](t, s, `SELECT count(*) FROM calls WHERE to_jsonb(calls)::text LIKE '%'||$1||'%'`, userSentinel); n != 0 {
					t.Error("message text leaked into the calls table")
				}
			}
		})
	}
}
