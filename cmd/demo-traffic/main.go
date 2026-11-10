// Command demo-traffic fills a local workspace with calls without spending money: it replays
// synthetic agent sessions (OpenAI and Anthropic wires, streamed and not, with tool calls and an
// upstream error; the shapes of the tollgate testkit fixtures) through a real tollgate proxy with the
// ledger hook, against a loopback fake upstream built into this command. No
// provider key exists anywhere in the run. The ledger writes calls and sessions into DATABASE_URL, so
// the dashboard, Setup status and /stats show numbers afterwards:
//
//	make db-up && go run ./cmd/meterwised migrate && make demo-traffic
//	go run ./cmd/demo-traffic -rounds 20 -workspace <workspace uuid>
//
// The workspace defaults to the organization's default workspace. Each round is one session of
// several calls; sessions are tagged with a fresh id per run.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/ledger"
	"github.com/ethosagent/meterwise/internal/savings"
	"github.com/ethosagent/meterwise/internal/store"
)

// script is one session's calls: a short agent run that ends in a text answer.
var script = []string{"openai-tools", "openai-text", "openai-json", "anthropic-text", "anthropic-json", "openai-500", "openai-text"}

// sessionHeader is the optional header a client may send to tag an agent run (Plan 05).
const sessionHeader = "x-meterwise-session"

type config struct {
	dsn       string
	workspace string
	rounds    int
}

func main() {
	var cfg config
	flag.StringVar(&cfg.workspace, "workspace", "", "workspace uuid (default: the default workspace)")
	flag.IntVar(&cfg.rounds, "rounds", 5, "sessions to replay")
	flag.Parse()
	cfg.dsn = os.Getenv("DATABASE_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := run(ctx, cfg, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "demo-traffic:", err)
		os.Exit(1)
	}
}

type tenantAuth struct{ ws, org string }

func (a tenantAuth) Authenticate(context.Context, *http.Request) (tollgate.Tenant, error) {
	return tollgate.Tenant{ID: a.ws, Attrs: map[string]string{"org_id": a.org}}, nil
}

type fixedResolver struct{ base *url.URL }

func (r fixedResolver) Resolve(_ context.Context, _ tollgate.Tenant, req *http.Request) (tollgate.Provider, string, error) {
	name, rest, _ := strings.Cut(strings.TrimPrefix(req.URL.Path, "/"), "/")
	wire := tollgate.WireOpenAIChat
	if name == "anthropic" {
		wire = tollgate.WireAnthropicMessages
	}
	return tollgate.Provider{Name: name, Wire: wire, BaseURL: r.base}, "/" + rest, nil
}

func run(ctx context.Context, cfg config, out io.Writer) error {
	if cfg.dsn == "" {
		return errors.New("set DATABASE_URL (see `make db-up`)")
	}
	if cfg.rounds < 1 || cfg.rounds > 1000 {
		return errors.New("-rounds must be between 1 and 1000")
	}
	st, err := store.Open(ctx, cfg.dsn)
	if err != nil {
		return fmt.Errorf("open the database: %w", err)
	}
	defer st.Close()
	ws, err := pickWorkspace(ctx, st, cfg.workspace)
	if err != nil {
		return err
	}
	pricer := savings.NewTablePricer(nil)
	if err := (&ledger.PriceLoader{Source: st, Pricer: pricer}).Load(ctx); err != nil {
		return fmt.Errorf("load pricing: %w", err)
	}
	hook, err := ledger.New(ctx, ledger.Options{Writer: st, Pricer: pricer, PrivacyMode: true})
	if err != nil {
		return err
	}
	up := newUpstream()
	defer up.Close()
	p, srv, err := startProxy(ctx, up.URL, ws, hook)
	if err != nil {
		return err
	}
	defer srv.Close()

	stamp := time.Now().UTC().Format("20060102T150405")
	sent, err := replay(ctx, srv, cfg.rounds, stamp)
	if err != nil {
		return err
	}
	srv.Close()
	if err := finish(ctx, p, hook, ws, stamp, cfg.rounds, sent); err != nil {
		return err
	}
	s := hook.Status()
	fmt.Fprintf(out, "replayed %d calls in %d sessions into workspace %s (flushed %v, dropped %v, unpriced %v)\n", sent, cfg.rounds, ws, s["flushed"], s["dropped"], s["unpriced_calls"])
	return nil
}

// finish lets the ledger see every call, closes the demo sessions and flushes.
func finish(ctx context.Context, p *tollgate.Proxy, hook *ledger.Hook, ws, stamp string, rounds, sent int) error {
	// After runs just behind the response: wait until the ledger has seen every call before closing.
	for deadline := time.Now().Add(10 * time.Second); hook.Status()["enqueued"].(int64) < int64(sent) && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	// tollgate closes a session after its quiet TTL (minutes); the demo is finished, so close them now.
	for r := range rounds {
		hook.OnSessionClosed(ctx, tollgate.Session{ID: sessionID(stamp, r), TenantID: ws})
	}
	if err := p.Close(ctx); err != nil {
		return err
	}
	return hook.Close(ctx)
}

func sessionID(stamp string, round int) string { return fmt.Sprintf("demo-%s-%d", stamp, round) }

// startProxy builds a real tollgate proxy for one workspace, with the ledger hook, in front of the fake upstream.
func startProxy(ctx context.Context, upstreamURL, ws string, hook *ledger.Hook) (*tollgate.Proxy, *httptest.Server, error) {
	base, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, nil, err
	}
	hdr := tollgate.DefaultHeaderNames()
	hdr.Session = sessionHeader
	p, err := tollgate.New(tollgate.Options{Auth: tenantAuth{ws: ws, org: store.OrgID()}, Providers: fixedResolver{base}, Metrics: tollgate.DiscardSink{}, Headers: hdr})
	if err != nil {
		return nil, nil, fmt.Errorf("proxy: %w", err)
	}
	p.Use(hook)
	if err := p.Start(ctx); err != nil {
		return nil, nil, err
	}
	return p, httptest.NewServer(p.Handler()), nil
}

// replay sends the script rounds times, one session per round, and returns how many calls it made.
func replay(ctx context.Context, srv *httptest.Server, rounds int, stamp string) (int, error) {
	sent := 0
	for r := range rounds {
		for _, name := range script {
			sc, _ := scenarioByName(name)
			if err := send(ctx, srv, sc, sessionID(stamp, r), sent); err != nil {
				return sent, err
			}
			sent++
		}
	}
	return sent, nil
}

func pickWorkspace(ctx context.Context, st *store.Store, id string) (string, error) {
	list, err := st.ListWorkspaces(ctx, store.OrgID())
	if err != nil {
		return "", fmt.Errorf("list workspaces: %w", err)
	}
	for _, w := range list {
		if (id == "" && w.IsDefault) || (id != "" && w.ID == id) {
			return w.ID, nil
		}
	}
	return "", errors.New("workspace not found (run `meterwised serve` once so the default workspace exists)")
}

func send(ctx context.Context, srv *httptest.Server, sc scenario, session string, n int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+sc.path, strings.NewReader(sc.request))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(scenarioHeader, sc.name)
	req.Header.Set(weightHeader, strconv.Itoa(n%9))
	req.Header.Set(sessionHeader, session)
	req.Header.Set("User-Agent", "demo-traffic/1")
	resp, err := srv.Client().Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", sc.name, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
