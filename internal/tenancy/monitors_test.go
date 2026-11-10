package tenancy_test

import (
	"context"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

func TestMonitorConfigReadUpdateAndCacheBust(t *testing.T) {
	dir, svc, _, _ := newDir(t)
	ctx := context.Background()
	ws, _ := svc.CreateWorkspace(ctx, principal(), "Alpha")
	if cfg, err := svc.MonitorConfig(ctx, principal(), ws.ID); err != nil || cfg != contract.DefaultMonitorConfig() {
		t.Fatalf("fresh config = %+v, %v", cfg, err)
	}
	if got, _ := dir.ByPublicID(ctx, ws.PublicID); got.Settings["monitors"] != nil { // fill the cache
		t.Fatalf("a fresh workspace has no stored monitors: %v", got.Settings)
	}
	mode := contract.ModeBlock
	steps := 9
	cfg, err := svc.UpdateMonitorConfig(ctx, principal(), ws.ID, contract.MonitorConfigPatch{
		StepBudget: &contract.StepBudgetPatch{Mode: &mode, MaxSteps: &steps},
	})
	if err != nil || cfg.StepBudget.Mode != contract.ModeBlock || cfg.StepBudget.MaxSteps != 9 || cfg.RepeatedRead != contract.DefaultMonitorConfig().RepeatedRead {
		t.Fatalf("update = %+v, %v", cfg, err)
	}
	// the cached directory entry was busted: the data plane sees the new config on its next resolve
	got, _ := dir.ByPublicID(ctx, ws.PublicID)
	if c := contract.MonitorConfigFrom(got.Settings["monitors"]); c != cfg {
		t.Fatalf("directory still serves %+v, want %+v", c, cfg)
	}
	bad := 0
	if _, err := svc.UpdateMonitorConfig(ctx, principal(), ws.ID, contract.MonitorConfigPatch{StepBudget: &contract.StepBudgetPatch{MaxSteps: &bad}}); contract.AsError(err).Code != contract.CodeValidationFailed {
		t.Fatalf("bad threshold: %v", err)
	}
	if again, _ := svc.MonitorConfig(ctx, principal(), ws.ID); again != cfg {
		t.Fatalf("a rejected update changed the config: %+v", again)
	}
	for _, id := range []string{"00000000-0000-0000-0000-00000000dead", "nope"} {
		if _, err := svc.MonitorConfig(ctx, principal(), id); contract.AsError(err).Code != contract.CodeNotFound {
			t.Errorf("MonitorConfig(%q): %v", id, err)
		}
		if _, err := svc.UpdateMonitorConfig(ctx, principal(), id, contract.MonitorConfigPatch{}); contract.AsError(err).Code != contract.CodeNotFound {
			t.Errorf("UpdateMonitorConfig(%q): %v", id, err)
		}
	}
}
