package audit_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/audit"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

func newSink(t *testing.T, now func() time.Time) *audit.Sink {
	t.Helper()
	ctx := context.Background()
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return audit.New(st, now)
}

func TestAppendFillsDefaultsAndListReturnsNewestFirst(t *testing.T) {
	ctx := context.Background()
	clk := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	s := newSink(t, func() time.Time { clk = clk.Add(time.Second); return clk })
	for _, a := range []contract.AuditAction{contract.AuditGroupCreate, contract.AuditWorkspaceURLReset, contract.AuditGroupDelete} {
		if err := s.Append(ctx, contract.AuditEvent{Action: a, ResourceType: "x", Metadata: map[string]any{"k": "v"}}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.List(ctx, contract.AuditQuery{})
	if err != nil || len(page.Items) != 3 || page.Items[0].Action != contract.AuditGroupDelete || page.Items[2].Action != contract.AuditGroupCreate {
		t.Fatalf("list: %+v %v", page, err)
	}
	if page.Items[0].Metadata["k"] != "v" || page.Items[0].At.IsZero() {
		t.Fatalf("row: %+v", page.Items[0])
	}
	only, _ := s.List(ctx, contract.AuditQuery{Action: contract.AuditWorkspaceURLReset})
	if len(only.Items) != 1 {
		t.Fatalf("filter: %+v", only)
	}
}

func TestUnknownActionsAreRefused(t *testing.T) {
	ctx := context.Background()
	s := newSink(t, nil)
	if err := s.Append(ctx, contract.AuditEvent{Action: "workspace.explode"}); err == nil {
		t.Fatal("an action outside the fixed list was appended")
	}
	if _, err := s.List(ctx, contract.AuditQuery{Action: "workspace.explode"}); contract.AsError(err).Code != contract.CodeValidationFailed {
		t.Fatalf("list with an unknown action: %v", err)
	}
}

type failing struct{}

func (failing) Append(context.Context, contract.AuditEvent) error {
	return errors.New("db down 203.0.113.9")
}

func TestEmitLogsAFailureWithoutLeakingTheEventAndNeverPanicsOnNil(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	audit.Emit(context.Background(), log, failing{}, contract.AuditEvent{
		Action: contract.AuditWorkspaceURLReset, IP: "203.0.113.9", Metadata: map[string]any{"publicId": "SECRETSECRETSECRETSECRET"},
	})
	out := buf.String()
	if !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "workspace.url_reset") {
		t.Fatalf("log: %q", out)
	}
	for _, leak := range []string{"SECRETSECRET", "203.0.113.9"} {
		if strings.Contains(out, leak) {
			t.Fatalf("log leaks %q: %s", leak, out)
		}
	}
	audit.Emit(context.Background(), log, nil, contract.AuditEvent{}) // nil sink: a no-op
	audit.Emit(context.Background(), nil, failing{}, contract.AuditEvent{})
}
