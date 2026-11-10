package ledger_test

import (
	"context"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"
	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/ledger"
	"github.com/ethosagent/meterwise/internal/nonet"
	"github.com/ethosagent/meterwise/internal/savings"
	"github.com/ethosagent/meterwise/internal/store"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

const (
	wsID  = "11111111-1111-4111-8111-111111111111"
	orgID = "00000000-0000-0000-0000-000000000001"
)

// tenantAuth resolves every call to one workspace, as wsauth does for /w/{id}.
type tenantAuth struct {
	ws     string // defaults to wsID
	policy *tollgate.Policy
}

func (a tenantAuth) Authenticate(context.Context, *http.Request) (tollgate.Tenant, error) {
	ws := a.ws
	if ws == "" {
		ws = wsID
	}
	return tollgate.Tenant{ID: ws, Attrs: map[string]string{"org_id": orgID}, Policy: a.policy}, nil
}

// fakeWriter records what the batcher hands the store, standing in for Postgres.
type fakeWriter struct {
	mu      sync.Mutex
	calls   []contract.CallRecord
	seen    map[string]bool
	closed  []string
	batches []int
	fail    func(recs []contract.CallRecord) error
	delay   time.Duration
}

func (w *fakeWriter) WriteCalls(_ context.Context, recs []contract.CallRecord) (store.WriteResult, error) {
	if w.delay > 0 {
		time.Sleep(w.delay)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail != nil {
		if err := w.fail(recs); err != nil {
			return store.WriteResult{}, err
		}
	}
	if w.seen == nil {
		w.seen = map[string]bool{}
	}
	var res store.WriteResult
	for _, r := range recs {
		if w.seen[r.ID] {
			res.Duplicates++
			continue
		}
		w.seen[r.ID] = true
		w.calls = append(w.calls, r)
		res.Inserted++
	}
	w.batches = append(w.batches, len(recs))
	return res, nil
}

func (w *fakeWriter) CloseSession(_ context.Context, ws, id string, _ time.Time) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = append(w.closed, ws+"/"+id)
	return true, nil
}

func (w *fakeWriter) snapshot() []contract.CallRecord {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]contract.CallRecord(nil), w.calls...)
}

var epoch = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

func testPricer() *savings.TablePricer {
	one := func(v int64) *contract.MicroUSD { m := contract.MicroUSD(v); return &m }
	return savings.NewTablePricer([]savings.PriceRow{
		{Provider: "openai", Model: "gpt-4o", InputPerM: 2_500_000, OutputPerM: 10_000_000, CachedPerM: one(1_250_000), EffectiveFrom: epoch},
		{Provider: "openai", Model: "gpt-4o-mini", InputPerM: 150_000, OutputPerM: 600_000, EffectiveFrom: epoch},
		{Provider: "openai", Model: "gpt-4.1-mini", InputPerM: 400_000, OutputPerM: 1_600_000, EffectiveFrom: epoch},
		{Provider: "anthropic", Model: "claude-sonnet-4-5", InputPerM: 3_000_000, OutputPerM: 15_000_000, EffectiveFrom: epoch},
		{Provider: "anthropic", Model: "claude-sonnet-4", InputPerM: 3_000_000, OutputPerM: 15_000_000, CachedPerM: one(300_000), EffectiveFrom: epoch},
	})
}

func newHook(t testing.TB, w ledger.Writer, mod func(*ledger.Options)) *ledger.Hook {
	t.Helper()
	o := ledger.Options{Writer: w, Pricer: testPricer(), FlushInterval: 10 * time.Millisecond}
	if mod != nil {
		mod(&o)
	}
	h, err := ledger.New(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// drive runs fixtures through a real tollgate proxy with the hook and the tenant authenticator,
// closes the hook (final flush) and returns the testkit recording.
func drive(t testing.TB, h *ledger.Hook, policy *tollgate.Policy, fixtures ...string) *testkit.Recording {
	t.Helper()
	return driveAs(t, h, tenantAuth{policy: policy}, fixtures...)
}

func driveAs(t testing.TB, h *ledger.Hook, auth tenantAuth, fixtures ...string) *testkit.Recording {
	t.Helper()
	rec := testkit.RunWith(t, testkit.Config{Plugins: []any{auth, h}, Fixtures: fixtures})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	return rec
}

// directCall builds a neutral Call the way tollgate hands it to a hook, for tests that drive Before/After themselves.
func directCall(id, text string, policy *tollgate.Policy, calls int) *tollgate.Call {
	return &tollgate.Call{
		ID: id, Tenant: tollgate.Tenant{ID: wsID, Attrs: map[string]string{"org_id": orgID}, Policy: policy},
		Session:  tollgate.Session{ID: "sess-1", TenantID: wsID, Source: tollgate.SessionFromHeader, StartedAt: time.Now(), Calls: calls},
		Provider: tollgate.Provider{Name: "openai", Wire: tollgate.WireOpenAIChat}, Model: "gpt-4o", Stream: true,
		Messages: []tollgate.Message{{Role: tollgate.RoleSystem, Text: "SYSTEM-PROMPT-TEXT"}, {Role: tollgate.RoleUser, Text: text}},
		Header:   http.Header{"User-Agent": {"opencode/1.2 (linux)"}, "Authorization": {"Bearer sk-secret-not-real"}}, Meta: map[string]any{},
	}
}

func metricFor(c *tollgate.Call) tollgate.Metric {
	return tollgate.Metric{CallID: c.ID, TenantID: c.Tenant.ID, SessionID: c.Session.ID, Provider: "openai", Model: c.Model, Stream: true, Status: 200,
		Finish: tollgate.FinishOK, Usage: tollgate.Usage{InputTokens: 10, OutputTokens: 5}, Start: time.Now(), TTFB: 5 * time.Millisecond, Duration: 50 * time.Millisecond}
}

func complete(t testing.TB, h *ledger.Hook, c *tollgate.Call) {
	t.Helper()
	if _, err := h.Before(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	h.After(context.Background(), c, metricFor(c))
}
