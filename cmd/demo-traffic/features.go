package main

import (
	"context"
	"fmt"
	"io"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/tenancy"
)

// features is what the three savers add to a demo run beyond the base chain: the config the rounds read and the
// chain options the seams fill (router tables, memory hook). The zero value is every saver off.
type features struct {
	cfg   config
	chain dataplane.ChainOptions
	mem   *memoryDemo // nil unless -memory is on
}

// checkFeatureModes refuses an unknown -trim, -router or -memory value before any work starts. An empty value is off.
func checkFeatureModes(cfg config) error {
	if m := contract.TrimMode(cfg.trim); cfg.trim != "" && !m.Valid() {
		return fmt.Errorf("-trim must be off, observe, lossless or aggressive, got %q", cfg.trim)
	}
	if m := contract.RouterMode(cfg.router); cfg.router != "" && !m.Valid() {
		return fmt.Errorf("-router must be off, observe, conservative or aggressive, got %q", cfg.router)
	}
	if m := contract.MemoryMode(cfg.memory); cfg.memory != "" && !m.Valid() {
		return fmt.Errorf("-memory must be off, observe or hint, got %q", cfg.memory)
	}
	return nil
}

// orOff is the mode a flag names; an empty flag is off.
func orOff[T ~string](v string) T {
	if v == "" {
		return T("off")
	}
	return T(v)
}

// setFeatures puts the workspace's trim, router and memory modes where the flags say (off by default), through
// the tenancy calls the PATCH handlers make (validation, row lock and cache bust included), and prints what it set.
// It runs before setMonitors, whose tenant is built from the stored row and so carries all four settings.
func setFeatures(ctx context.Context, st *store.Store, ws string, cfg config, out io.Writer) error {
	svc := tenancy.New(st, nil, tenancy.Config{MaxWorkspaces: 1})
	p := contract.Principal{OrgID: store.OrgID()}
	tm, rm, mm := orOff[contract.TrimMode](cfg.trim), orOff[contract.RouterMode](cfg.router), orOff[contract.MemoryMode](cfg.memory)
	if _, err := svc.UpdateTrimConfig(ctx, p, ws, contract.TrimConfigPatch{Mode: &tm}); err != nil {
		return fmt.Errorf("set trim: %w", err)
	}
	if _, err := svc.UpdateRouterConfig(ctx, p, ws, contract.RouterConfigPatch{Mode: &rm}); err != nil {
		return fmt.Errorf("set router: %w", err)
	}
	if _, err := svc.UpdateMemoryConfig(ctx, p, ws, contract.MemoryConfigPatch{Mode: &mm}); err != nil {
		return fmt.Errorf("set memory: %w", err)
	}
	fmt.Fprintf(out, "savers set on workspace %s: trim=%s router=%s memory=%s\n", ws, tm, rm, mm)
	return nil
}
