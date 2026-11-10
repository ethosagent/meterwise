package main

import (
	"context"
	"fmt"
	"io"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// summarize prints what this run left in the workspace: calls, sessions, monitor events by action and
// confidence, and the two savings numbers. Certain-saved and likely-saved are printed as two numbers and
// never added. Every query is scoped by workspace_id and to this run's sessions (ids start with prefix).
func summarize(ctx context.Context, st *store.Store, ws, prefix string, out io.Writer) error {
	like := prefix + "%"
	var calls, blocked, sessions int
	var saved, likely string
	pool := st.Pool()
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE blocked) FROM calls WHERE workspace_id = $1 AND session_id LIKE $2`, ws, like).Scan(&calls, &blocked); err != nil {
		return fmt.Errorf("summary calls: %w", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*), COALESCE(sum(saved_usd), 0)::text, COALESCE(sum(likely_saved_usd), 0)::text FROM sessions WHERE workspace_id = $1 AND id LIKE $2`, ws, like).Scan(&sessions, &saved, &likely); err != nil {
		return fmt.Errorf("summary sessions: %w", err)
	}
	rows, err := pool.Query(ctx, `SELECT action, confidence, count(*), COALESCE(sum(est_saved_usd), 0)::text FROM monitor_events WHERE workspace_id = $1 AND session_id LIKE $2 GROUP BY action, confidence ORDER BY action, confidence`, ws, like)
	if err != nil {
		return fmt.Errorf("summary events: %w", err)
	}
	defer rows.Close()
	fmt.Fprintf(out, "summary: %d calls (%d blocked), %d sessions\nmonitor events by action and confidence:\n", calls, blocked, sessions)
	events := 0
	for rows.Next() {
		var action, conf, usd string
		var n int
		if err := rows.Scan(&action, &conf, &n, &usd); err != nil {
			return err
		}
		events += n
		fmt.Fprintf(out, "  %-8s %-8s %4d   est %s\n", action, conf, n, usd)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if events == 0 {
		fmt.Fprintln(out, "  (none)")
	}
	fmt.Fprintf(out, "saved (certain): $%s    likely saved: $%s    (two numbers, never summed; observe rows are would-save, in neither)\n", money(saved), money(likely))
	return nil
}

// money renders a numeric column as the six-place decimal MicroUSD prints.
func money(s string) string {
	m, err := contract.ParseMicroUSD(s)
	if err != nil {
		return s
	}
	return m.Decimal()
}
