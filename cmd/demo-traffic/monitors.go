package main

import (
	"context"
	"fmt"
	"io"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/tenancy"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

// defaultMonitors is the -monitors default: notes reach the agent, nothing is blocked.
const defaultMonitors = contract.ModeNote

// parseMonitors reads the -monitors flag. Only observe, note and block are modes.
func parseMonitors(s string) (contract.MonitorMode, error) {
	if s == "" {
		return defaultMonitors, nil
	}
	if m := contract.MonitorMode(s); m.Valid() {
		return m, nil
	}
	return "", fmt.Errorf("-monitors must be observe, note or block, got %q", s)
}

// modeConfig is the default monitor config with every monitor in mode. A monitor that cannot block acts as
// note in block mode, as it does in production.
func modeConfig(mode contract.MonitorMode) contract.MonitorConfig {
	c := contract.DefaultMonitorConfig()
	c.DuplicateToolCall.Mode, c.RepeatedRead.Mode, c.NoProgress.Mode = mode, mode, mode
	c.StepBudget.Mode, c.ReasoningLoop.Mode, c.SecondGuessing.Mode = mode, mode, mode
	return c
}

// modePatch is modeConfig as the PATCH /api/workspaces/{id}/monitors body.
func modePatch(mode contract.MonitorMode) contract.MonitorConfigPatch {
	m := func() *contract.MonitorMode { v := mode; return &v }
	return contract.MonitorConfigPatch{
		DuplicateToolCall: &contract.DuplicateToolCallPatch{Mode: m()},
		RepeatedRead:      &contract.RepeatedReadPatch{Mode: m()},
		NoProgress:        &contract.NoProgressPatch{Mode: m()},
		StepBudget:        &contract.StepBudgetPatch{Mode: m()},
		ReasoningLoop:     &contract.ReasoningLoopPatch{Mode: m()},
		SecondGuessing:    &contract.SecondGuessingPatch{Mode: m()},
	}
}

// setMonitors puts every monitor of workspace ws in mode through tenancy.Service.UpdateMonitorConfig, the
// call the PATCH handler makes (validation, row lock and cache bust included), prints what it set and
// returns the workspace's tenant as the data plane sees it, built by wsauth.Resolve from the stored row.
func setMonitors(ctx context.Context, st *store.Store, ws string, mode contract.MonitorMode, out io.Writer) (tollgate.Tenant, error) {
	svc := tenancy.New(st, nil, tenancy.Config{MaxWorkspaces: 1})
	p := contract.Principal{OrgID: store.OrgID()}
	cfg, err := svc.UpdateMonitorConfig(ctx, p, ws, modePatch(mode))
	if err != nil {
		return tollgate.Tenant{}, fmt.Errorf("set the monitors: %w", err)
	}
	fmt.Fprintf(out, "monitors set to %s on workspace %s: %s\n", mode, ws, describe(cfg))
	list, err := svc.Directory().List(ctx, p)
	if err != nil {
		return tollgate.Tenant{}, fmt.Errorf("list workspaces: %w", err)
	}
	for _, w := range list {
		if w.ID == ws {
			return wsauth.Resolve(w).(*wsauth.Resolved).Tenant, nil
		}
	}
	return tollgate.Tenant{}, fmt.Errorf("workspace %s not found", ws)
}

// describe lists the mode of every monitor, in the order of contract.MonitorNames.
func describe(c contract.MonitorConfig) string {
	return fmt.Sprintf("duplicate_tool_call=%s repeated_read=%s no_progress=%s step_budget=%s reasoning_loop=%s second_guessing=%s",
		c.DuplicateToolCall.Mode, c.RepeatedRead.Mode, c.NoProgress.Mode, c.StepBudget.Mode, c.ReasoningLoop.Mode, c.SecondGuessing.Mode)
}
