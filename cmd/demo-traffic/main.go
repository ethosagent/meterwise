// Command demo-traffic fills a local workspace with calls without spending money: it replays
// synthetic agent sessions (OpenAI and Anthropic wires, streamed and not, with tool calls and an
// upstream error; the shapes of the tollgate testkit fixtures) through a real tollgate proxy with the
// hook chain serve uses (history, monitors, ledger; dataplane.HookChain), against a loopback fake upstream
// built into this command. No provider key exists anywhere in the run. The ledger writes calls, sessions
// and monitor events into DATABASE_URL, so the dashboard, Savings, Monitors and /stats show numbers afterwards:
//
//	make db-up && go run ./cmd/meterwised migrate && make demo-traffic
//	go run ./cmd/demo-traffic -rounds 20 -workspace <workspace uuid> -monitors block
//
// The workspace defaults to the organization's default workspace. Each round is one plain session plus
// three looping agents (repeated tool call, repeated read, no progress; both wires, streamed or not).
// -monitors observe|note|block (default note) sets every monitor of the workspace first, the way PATCH
// /api/workspaces/{id}/monitors does; block mode stops the loops and moves the saved number. A summary of
// the run is printed at the end.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

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
	monitors  string // observe|note|block; "" is the default (note)
}

func main() {
	var cfg config
	flag.StringVar(&cfg.workspace, "workspace", "", "workspace uuid (default: the default workspace)")
	flag.IntVar(&cfg.rounds, "rounds", 5, "rounds to replay (each: one plain session and three looping agents)")
	flag.StringVar(&cfg.monitors, "monitors", string(defaultMonitors), "monitor mode to set on the workspace first: observe|note|block")
	flag.Parse()
	cfg.dsn = os.Getenv("DATABASE_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if _, err := run(ctx, cfg, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "demo-traffic:", err)
		os.Exit(1)
	}
}

// checkConfig refuses bad input before any database or proxy work starts.
func checkConfig(cfg config) error {
	if cfg.dsn == "" {
		return errors.New("set DATABASE_URL (see `make db-up`)")
	}
	if cfg.rounds < 1 || cfg.rounds > 1000 {
		return errors.New("-rounds must be between 1 and 1000")
	}
	if _, err := parseMonitors(cfg.monitors); err != nil {
		return err
	}
	return validateScript(script)
}

// newHook loads the price table and builds the ledger hook that writes into st, with serve's estimator.
func newHook(ctx context.Context, st *store.Store) (*ledger.Hook, error) {
	pricer := savings.NewTablePricer(nil)
	if err := (&ledger.PriceLoader{Source: st, Pricer: pricer}).Load(ctx); err != nil {
		return nil, fmt.Errorf("load pricing: %w", err)
	}
	privacyOn := true
	return ledger.New(ctx, ledger.Options{Writer: st, Pricer: pricer, Estimator: savings.Estimate, PrivacyMode: &privacyOn})
}

func run(ctx context.Context, cfg config, out io.Writer) (result, error) {
	if err := checkConfig(cfg); err != nil {
		return result{}, err
	}
	mode, _ := parseMonitors(cfg.monitors)
	st, err := store.Open(ctx, cfg.dsn)
	if err != nil {
		return result{}, fmt.Errorf("open the database: %w", err)
	}
	defer st.Close()
	ws, err := pickWorkspace(ctx, st, cfg.workspace)
	if err != nil {
		return result{}, err
	}
	tenant, err := setMonitors(ctx, st, ws, mode, out)
	if err != nil {
		return result{}, err
	}
	hook, err := newHook(ctx, st)
	if err != nil {
		return result{}, err
	}
	stamp := time.Now().UTC().Format("20060102T150405")
	res, err := play(ctx, tenant, hook, cfg.rounds, stamp)
	if err != nil {
		return result{}, err
	}
	s := hook.Status()
	fmt.Fprintf(out, "replayed %d calls in %d sessions into workspace %s (reached the upstream: %d; flushed %v, dropped %v, unpriced %v)\n",
		res.clientCalls, len(res.sessions), ws, res.upstreamHits, s["flushed"], s["dropped"], s["unpriced_calls"])
	return res, summarize(ctx, st, ws, "demo-"+stamp+"-", out)
}

// validateScript refuses a script that names a scenario the fake upstream does not know, before any
// database or proxy work starts.
func validateScript(names []string) error {
	for _, name := range names {
		if _, ok := scenarioByName(name); !ok {
			return fmt.Errorf("demo-traffic: unknown scenario %q in the script", name)
		}
	}
	return nil
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
