package api

import (
	"context"
	"slices"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/savings"
)

// Savings is what the control plane reads for the savings dashboard; *store.Store implements it. Every read
// is scoped by the workspace id the gateway authorized, and nothing here can change a row. Mode and Confidence
// of the CauseView rows (and the Upsell list) are left for this package to fill from the monitor settings.
type Savings interface {
	Overview(ctx context.Context, workspaceID string, r contract.SavingsRange) (contract.SavingsOverview, error)
	Causes(ctx context.Context, workspaceID string, r contract.SavingsRange) (contract.SavingsCauses, []contract.RepeatTarget, error)
	Saves(ctx context.Context, workspaceID string, r contract.SavingsRange, cursor string, limit int) (contract.SaveList, error)
}

// defaultSaves is the first page of the biggest-saves table; the bounds are the shared page limit.
const defaultSaves = 20

// registerSavings wires the Plan 07 read routes. The gateway has authorized savings:read on the workspace.
func (s *server) registerSavings() {
	register(s, "GET", "/api/workspaces/{id}/savings/overview", s.savingsOverview)
	register(s, "GET", "/api/workspaces/{id}/savings/causes", s.savingsCauses)
	register(s, "GET", "/api/workspaces/{id}/savings/saves", s.savingsSaves)
}

// savingsReader is the wired port, or not_available.
func (s *server) savingsReader() (Savings, error) {
	if s.Savings == nil {
		return nil, contract.Unavailable("the savings read model", "")
	}
	return s.Savings, nil
}

// savingsRange validates ?range=&from=&to= (ruling S5) before anything is read.
func (s *server) savingsRange(c Call) (contract.SavingsRange, error) {
	return contract.ParseSavingsRange(c.Query("range"), c.Query("from"), c.Query("to"), s.now())
}

func (s *server) savingsOverview(c Call, _ none) (contract.SavingsOverview, error) {
	sv, err := s.savingsReader()
	if err != nil {
		return contract.SavingsOverview{}, err
	}
	r, err := s.savingsRange(c)
	if err != nil {
		return contract.SavingsOverview{}, err
	}
	cfg, err := s.causeModes(c)
	if err != nil {
		return contract.SavingsOverview{}, err
	}
	out, err := sv.Overview(c.Ctx, c.Path("id"), r)
	if err != nil {
		return contract.SavingsOverview{}, err
	}
	for i := range out.ByCause {
		fillCause(cfg, &out.ByCause[i])
	}
	out.Upsell = upsellOf(out.ByCause)
	return out, nil
}

func (s *server) savingsCauses(c Call, _ none) (contract.SavingsCauses, error) {
	sv, err := s.savingsReader()
	if err != nil {
		return contract.SavingsCauses{}, err
	}
	r, err := s.savingsRange(c)
	if err != nil {
		return contract.SavingsCauses{}, err
	}
	cfg, err := s.causeModes(c)
	if err != nil {
		return contract.SavingsCauses{}, err
	}
	out, targets, err := sv.Causes(c.Ctx, c.Path("id"), r)
	if err != nil {
		return contract.SavingsCauses{}, err
	}
	for i := range out.Causes {
		fillCause(cfg, &out.Causes[i].CauseView)
	}
	out.Suggestions = savings.Explain(targets)
	return out, nil
}

func (s *server) savingsSaves(c Call, _ none) (contract.SaveList, error) {
	sv, err := s.savingsReader()
	if err != nil {
		return contract.SaveList{}, err
	}
	r, err := s.savingsRange(c)
	if err != nil {
		return contract.SaveList{}, err
	}
	limit, err := contract.ParsePageLimit(c.Query("limit"))
	if err != nil {
		return contract.SaveList{}, err
	}
	if limit == 0 {
		limit = defaultSaves
	}
	return sv.Saves(c.Ctx, c.Path("id"), r, c.Query("cursor"), limit)
}

// modes are the four feature settings a cause row's Mode and Confidence come from.
type modes struct {
	monitors contract.MonitorConfig
	trim     contract.TrimMode
	router   contract.RouterMode
	memory   contract.MemoryMode
}

// causeModes reads the workspace's monitor, trim, router and memory settings (each one a cached read).
func (s *server) causeModes(c Call) (modes, error) {
	var m modes
	var err error
	if m.monitors, err = s.Tenancy.MonitorConfig(c.Ctx, c.Principal, c.Path("id")); err != nil {
		return m, err
	}
	t, err := s.Tenancy.TrimConfig(c.Ctx, c.Principal, c.Path("id"))
	if err != nil {
		return m, err
	}
	r, err := s.Tenancy.RouterConfig(c.Ctx, c.Principal, c.Path("id"))
	if err != nil {
		return m, err
	}
	mem, err := s.Tenancy.MemoryConfig(c.Ctx, c.Principal, c.Path("id"))
	if err != nil {
		return m, err
	}
	m.trim, m.router, m.memory = t.Mode, r.Mode, mem.Mode
	return m, nil
}

// fillCause sets a row's configured Mode and the class the cause gives: a monitor's comes from the estimator
// (savings.Estimate with an empty basis: only the confidence is read), trim and router savings are certain
// where they are measured, and memory's are likely (ADR 0027).
func fillCause(m modes, row *contract.CauseView) {
	switch row.Cause.Kind() {
	case contract.KindTrim:
		row.Mode, row.Confidence = string(m.trim), contract.ConfidenceCertain
	case contract.KindRouter:
		row.Mode, row.Confidence = string(m.router), contract.ConfidenceCertain
	case contract.KindMemory:
		row.Mode, row.Confidence = string(m.memory), contract.ConfidenceLikely
	default:
		name, _ := row.Cause.Monitor()
		mode := m.monitors.ModeOf(name)
		_, conf := savings.Estimate(contract.MonitorVerdict{Monitor: name, Mode: mode, Action: mode})
		row.Mode, row.Confidence = string(mode), conf
	}
}

// upsellOf lists the monitors that are in observe mode and would have saved something, biggest first. Likely
// and acted money never enter it: the list shows only the would-save class.
func upsellOf(rows []contract.CauseView) []contract.UpsellView {
	out := []contract.UpsellView{}
	for _, r := range rows {
		if r.Mode == string(contract.ModeObserve) && r.WouldSaveUsd > 0 {
			out = append(out, contract.UpsellView{Cause: r.Cause, WouldSaveUsd: r.WouldSaveUsd, WouldSaveTokens: r.WouldSaveTokens, Fires: r.Fires})
		}
	}
	slices.SortStableFunc(out, func(a, b contract.UpsellView) int {
		switch {
		case a.WouldSaveUsd > b.WouldSaveUsd:
			return -1
		case a.WouldSaveUsd < b.WouldSaveUsd:
			return 1
		}
		return 0
	})
	return out
}

// insightOf is SessionDetail.insight: the repetitions the session's monitor events name, explained, and the
// one suggestion line (nil when there is none). A path is named only when the event stored one (R7).
func insightOf(events []contract.MonitorEventRecord) contract.SessionInsight {
	type key struct {
		m                contract.MonitorName
		tool, path, hash string
	}
	counts := map[key]int64{}
	var order []key
	for _, e := range events {
		if !e.Monitor.IsRepetition() || e.Evidence.Tool == "" {
			continue
		}
		k := key{e.Monitor, e.Evidence.Tool, e.Evidence.Path, e.Evidence.PathHash}
		if counts[k] == 0 {
			order = append(order, k)
		}
		counts[k]++
	}
	targets := make([]contract.RepeatTarget, len(order))
	for i, k := range order {
		targets[i] = contract.RepeatTarget{Monitor: k.m, Tool: k.tool, Path: k.path, PathHash: k.hash, Count: counts[k]}
	}
	rep := savings.Explain(targets)
	return contract.SessionInsight{Repeats: rep, Suggestion: savings.Primary(rep)}
}
