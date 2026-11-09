package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strings"
	"syscall"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
)

// Exit codes.
const (
	exitOK     = 0
	exitFailed = 1
	exitUsage  = 2
)

// command is one row of the subcommand table.
type command struct {
	name    string
	summary string
	run     func(ctx context.Context, e *env, args []string) int
}

// env is everything a subcommand needs from the process, injected for tests.
type env struct {
	lookup config.Lookup
	stdout io.Writer
	stderr io.Writer
	deps   dataplane.Deps // data-plane overrides; zero in production, set by tests
}

func commands() []command {
	return []command{
		{"serve", "run the proxy, the control-plane API and the web app", cmdServe},
		{"config", "config check: validate the environment and print the effective config", cmdConfig},
		{"bootstrap-admin", "bootstrap-admin --email <address>: invite the first admin", cmdBootstrapAdmin},
		{"migrate", "migrate [up|status]: apply or list database migrations", cmdMigrate},
		{"version", "print the build version", cmdVersion},
	}
}

func run(args []string, lookup config.Lookup, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runCtx(ctx, args, lookup, stdout, stderr)
}

func runCtx(ctx context.Context, args []string, lookup config.Lookup, stdout, stderr io.Writer) int {
	e := &env{lookup: lookup, stdout: stdout, stderr: stderr}
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		return cmdHelp(e, args)
	}
	for _, c := range commands() {
		if c.name == args[0] {
			return c.run(ctx, e, args[1:])
		}
	}
	return fail(e, false, contract.NewError(contract.CodeInvalidRequest, "unknown command "+args[0]), exitUsage)
}

func cmdHelp(e *env, args []string) int {
	asJSON := len(args) > 1 && (args[1] == "-json" || args[1] == "--json")
	type row struct {
		Name    string `json:"name"`
		Summary string `json:"summary"`
	}
	rows := make([]row, 0, 5)
	for _, c := range commands() {
		rows = append(rows, row{c.name, c.summary})
	}
	if asJSON {
		return ok(e, true, map[string]any{"commands": rows})
	}
	var b strings.Builder
	b.WriteString("usage: meterwised <command> [-json]\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-16s %s\n", r.Name, r.Summary)
	}
	_, _ = io.WriteString(e.stdout, b.String())
	return exitOK
}

// parseFlags parses a subcommand's arguments with the standard flag package. -json is always
// registered; register adds the command's own flags. Flags may follow positionals (`config
// check -json` and `config -json check` both work); whatever is not a flag is returned in rest.
// A parse error is a usage error, answered as a JSON error when -json was asked, whichever
// command it came from.
func parseFlags(e *env, name string, args []string, register func(*flag.FlagSet)) (asJSON bool, rest []string, code int) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonFlag := fs.Bool("json", false, "machine-readable output")
	if register != nil {
		register(fs)
	}
	for todo := args; ; {
		if err := fs.Parse(todo); err != nil {
			wantJSON := slices.Contains(args, "-json") || slices.Contains(args, "--json")
			return false, nil, fail(e, wantJSON, contract.NewError(contract.CodeInvalidRequest, name+": "+err.Error()), exitUsage)
		}
		todo = fs.Args()
		if len(todo) == 0 {
			break
		}
		rest, todo = append(rest, todo[0]), todo[1:]
	}
	return *jsonFlag, rest, exitOK
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  any             `json:"data,omitempty"`
	Error *contract.Error `json:"error,omitempty"`
}

// ok prints data as one JSON document, or via the text renderer when asJSON is false.
func ok(e *env, asJSON bool, data any) int {
	if asJSON {
		return emit(e.stdout, envelope{OK: true, Data: data})
	}
	return emitText(e.stdout, data)
}

func fail(e *env, asJSON bool, err *contract.Error, code int) int {
	if asJSON {
		emit(e.stdout, envelope{Error: err})
		return code
	}
	fmt.Fprintf(e.stderr, "error %s: %s\n", err.Code, err.Message)
	for _, d := range err.Details {
		fmt.Fprintf(e.stderr, "  - %s\n", d)
	}
	return code
}

func emit(w io.Writer, v envelope) int {
	b, err := json.Marshal(v)
	if err != nil {
		b = []byte(`{"ok":false,"error":{"code":"internal","message":"internal error"}}`)
	}
	_, _ = w.Write(append(b, '\n'))
	return exitOK
}

func emitText(w io.Writer, data any) int {
	switch d := data.(type) {
	case map[string]string:
		keys := make([]string, 0, len(d))
		for k := range d {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "%s=%s\n", k, d[k])
		}
	default:
		b, _ := json.MarshalIndent(d, "", "  ")
		_, _ = w.Write(append(b, '\n'))
	}
	return exitOK
}
