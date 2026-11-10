package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/tenancy"
)

func TestFeatureFlagsAreValidatedBeforeAnyWork(t *testing.T) {
	for _, c := range []struct {
		cfg  config
		want string
	}{
		{config{dsn: "x", rounds: 1, trim: "banana"}, "-trim"},
		{config{dsn: "x", rounds: 1, router: "yolo"}, "-router"},
		{config{dsn: "x", rounds: 1, memory: "always"}, "-memory"},
	} {
		if err := checkConfig(c.cfg); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: err = %v, want one naming %s", c.cfg, err, c.want)
		}
	}
	for _, ok := range []config{{dsn: "x", rounds: 1}, {dsn: "x", rounds: 1, trim: "lossless", router: "conservative", memory: "hint"}} {
		if err := checkConfig(ok); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
}

// The savers default to off: a run resets the workspace to off (the Overview then shows them "Off"), and each flag
// sets its own mode through tenancy, leaving the other two settings alone.
func TestFeatureFlagsSetTheWorkspaceModes(t *testing.T) {
	ctx := context.Background()
	dsn, st, ws := demoDB(t)
	svc := tenancy.New(st, nil, tenancy.Config{MaxWorkspaces: 3})
	p := contract.Principal{OrgID: store.OrgID()}
	modes := func() (contract.TrimMode, contract.RouterMode, contract.MemoryMode) {
		tc, err := svc.TrimConfig(ctx, p, ws)
		if err != nil {
			t.Fatal(err)
		}
		rc, _ := svc.RouterConfig(ctx, p, ws)
		mc, _ := svc.MemoryConfig(ctx, p, ws)
		return tc.Mode, rc.Mode, mc.Mode
	}
	if tm, _, _ := modes(); tm != contract.TrimModeObserve {
		t.Fatalf("a fresh workspace trims in observe, got %s", tm)
	}
	var out bytes.Buffer
	if _, err := run(ctx, config{dsn: dsn, rounds: 1}, &out); err != nil {
		t.Fatal(err)
	}
	if tm, rm, mm := modes(); tm != contract.TrimModeOff || rm != contract.RouterModeOff || mm != contract.MemoryModeOff {
		t.Errorf("default flags: trim=%s router=%s memory=%s, want all off", tm, rm, mm)
	}
	if !strings.Contains(out.String(), "trim=off router=off memory=off") {
		t.Errorf("the run does not say what it set: %s", out.String())
	}
	out.Reset()
	if _, err := run(ctx, config{dsn: dsn, rounds: 1, trim: "lossless", router: "conservative", memory: "hint"}, &out); err != nil {
		t.Fatal(err)
	}
	if tm, rm, mm := modes(); tm != contract.TrimModeLossless || rm != contract.RouterModeConservative || mm != contract.MemoryModeHint {
		t.Errorf("flags: trim=%s router=%s memory=%s", tm, rm, mm)
	}
}
