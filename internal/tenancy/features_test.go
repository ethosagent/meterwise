package tenancy_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

// The three saver settings: defaults on a fresh workspace, a validated merge, a busted directory cache, 422 with
// nothing written, not_found outside the org, and every saver key surviving a settings replacement.
func TestSaverSettingsReadUpdateAndCacheBust(t *testing.T) {
	dir, svc, _, _ := newDir(t)
	ctx, p := context.Background(), principal()
	ws, _ := svc.CreateWorkspace(ctx, p, "Alpha")

	if c, err := svc.TrimConfig(ctx, p, ws.ID); err != nil || !reflect.DeepEqual(c, contract.DefaultTrimConfig()) {
		t.Fatalf("fresh trim = %+v, %v", c, err)
	}
	if c, err := svc.RouterConfig(ctx, p, ws.ID); err != nil || !reflect.DeepEqual(c, contract.DefaultRouterConfig()) {
		t.Fatalf("fresh router = %+v, %v", c, err)
	}
	if c, err := svc.MemoryConfig(ctx, p, ws.ID); err != nil || c != contract.DefaultMemoryConfig() {
		t.Fatalf("fresh memory = %+v, %v", c, err)
	}
	dir.ByPublicID(ctx, ws.PublicID) // fill the cache

	trimMode, routerMode, memMode := contract.TrimModeLossless, contract.RouterModeObserve, contract.MemoryModeObserve
	trim, err := svc.UpdateTrimConfig(ctx, p, ws.ID, contract.TrimConfigPatch{Mode: &trimMode})
	if err != nil || trim.Mode != contract.TrimModeLossless || trim.MaxFoldPct != 90 {
		t.Fatalf("trim update = %+v, %v", trim, err)
	}
	router, err := svc.UpdateRouterConfig(ctx, p, ws.ID, contract.RouterConfigPatch{Mode: &routerMode})
	if err != nil || router.Mode != contract.RouterModeObserve {
		t.Fatalf("router update = %+v, %v", router, err)
	}
	mem, err := svc.UpdateMemoryConfig(ctx, p, ws.ID, contract.MemoryConfigPatch{Mode: &memMode})
	if err != nil || mem.Mode != contract.MemoryModeObserve {
		t.Fatalf("memory update = %+v, %v", mem, err)
	}

	got, _ := dir.ByPublicID(ctx, ws.PublicID) // each update busted the cache
	if !reflect.DeepEqual(contract.TrimConfigFrom(got.Settings["trim"]), trim) || !reflect.DeepEqual(contract.RouterConfigFrom(got.Settings["router"]), router) ||
		contract.MemoryConfigFrom(got.Settings["memory"]) != mem {
		t.Fatalf("directory serves stale settings: %v", got.Settings)
	}

	// a settings replacement keeps all three and drops the request's own copies
	repl, err := svc.UpdateWorkspace(ctx, p, ws.ID, contract.UpdateWorkspaceRequest{Settings: map[string]any{
		"theme": "x", "trim": map[string]any{"mode": "aggressive"}, "router": map[string]any{"mode": "aggressive"}, "memory": map[string]any{"mode": "hint"},
	}})
	if err != nil || repl.Settings["theme"] != "x" {
		t.Fatalf("replace = %v, %v", repl.Settings, err)
	}
	if !reflect.DeepEqual(contract.TrimConfigFrom(repl.Settings["trim"]), trim) || !reflect.DeepEqual(contract.RouterConfigFrom(repl.Settings["router"]), router) ||
		contract.MemoryConfigFrom(repl.Settings["memory"]) != mem {
		t.Fatalf("a settings replace changed a saver key: %v", repl.Settings)
	}

	// 422 names the field and writes nothing
	badFold := 5
	_, err = svc.UpdateTrimConfig(ctx, p, ws.ID, contract.TrimConfigPatch{MaxFoldPct: &badFold})
	if e := contract.AsError(err); e.Code != contract.CodeValidationFailed || len(e.Details) == 0 || e.Details[0][:11] != "maxFoldPct:" {
		t.Fatalf("bad trim: %v", err)
	}
	if again, _ := svc.TrimConfig(ctx, p, ws.ID); !reflect.DeepEqual(again, trim) {
		t.Fatalf("a rejected update changed trim: %+v", again)
	}
	badThr := 99999
	if _, err := svc.UpdateRouterConfig(ctx, p, ws.ID, contract.RouterConfigPatch{ThresholdBp: &badThr}); contract.AsError(err).Code != contract.CodeValidationFailed {
		t.Fatalf("bad router: %v", err)
	}
	badHints := 9
	if _, err := svc.UpdateMemoryConfig(ctx, p, ws.ID, contract.MemoryConfigPatch{MaxHintsPerSession: &badHints}); contract.AsError(err).Code != contract.CodeValidationFailed {
		t.Fatalf("bad memory: %v", err)
	}
	// monitors are untouched by all of it
	if m, _ := svc.MonitorConfig(ctx, p, ws.ID); m != contract.DefaultMonitorConfig() {
		t.Fatalf("monitors changed: %+v", m)
	}

	for _, id := range []string{"00000000-0000-0000-0000-00000000dead", "nope"} {
		if _, err := svc.TrimConfig(ctx, p, id); contract.AsError(err).Code != contract.CodeNotFound {
			t.Errorf("TrimConfig(%q): %v", id, err)
		}
		if _, err := svc.UpdateRouterConfig(ctx, p, id, contract.RouterConfigPatch{}); contract.AsError(err).Code != contract.CodeNotFound {
			t.Errorf("UpdateRouterConfig(%q): %v", id, err)
		}
		if _, err := svc.UpdateMemoryConfig(ctx, p, id, contract.MemoryConfigPatch{}); contract.AsError(err).Code != contract.CodeNotFound {
			t.Errorf("UpdateMemoryConfig(%q): %v", id, err)
		}
	}
}
