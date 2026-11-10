package api

import (
	"context"
	"slices"
	"strconv"

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

const (
	defaultSaves = 20
	maxSaves     = 100
)

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
	cfg, err := s.Tenancy.MonitorConfig(c.Ctx, c.Principal, c.Path("id"))
	if err != nil {
		return contract.SavingsOverview{}, err
	}
	out, err := sv.Overview(c.Ctx, c.Path("id"), r)
	if err != nil {
		return contract.SavingsOverview{}, err
	}
	fillCauses(cfg, out.ByCause)
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
	cfg, err := s.Tenancy.MonitorConfig(c.Ctx, c.Principal, c.Path("id"))
	if err != nil {
		return contract.SavingsCauses{}, err
	}
	out, targets, err := sv.Causes(c.Ctx, c.Path("id"), r)
	if err != nil {
		return contract.SavingsCauses{}, err
	}
	fillCauses(cfg, out.Causes)
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
	limit := defaultSaves
	if raw := c.Query("limit"); raw != "" {
		if limit, err = strconv.Atoi(raw); err != nil || limit < 1 || limit > maxSaves {
			return contract.SaveList{}, contract.Invalid("limit", "must be a number from 1 to 100")
		}
	}
	return sv.Saves(c.Ctx, c.Path("id"), r, c.Query("cursor"), limit)
}

// fillCauses sets each row's configured Mode and the class the estimator gives that monitor in that mode
// (savings.Estimate with an empty basis: only the confidence is read).
func fillCauses(cfg contract.MonitorConfig, rows []contract.CauseView) {
	for i := range rows {
		m := cfg.ModeOf(rows[i].Monitor)
		_, conf := savings.Estimate(contract.MonitorVerdict{Monitor: rows[i].Monitor, Mode: m, Action: m})
		rows[i].Mode, rows[i].Confidence = m, conf
	}
}

// upsellOf lists the monitors that are in observe mode and would have saved something, biggest first. Likely
// and acted money never enter it: the list shows only the would-save class.
func upsellOf(rows []contract.CauseView) []contract.UpsellView {
	out := []contract.UpsellView{}
	for _, r := range rows {
		if r.Mode == contract.ModeObserve && r.WouldSaveUsd > 0 {
			out = append(out, contract.UpsellView{Monitor: r.Monitor, WouldSaveUsd: r.WouldSaveUsd, WouldSaveTokens: r.WouldSaveTokens, Fires: r.Fires})
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
