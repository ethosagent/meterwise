package api

import (
	"strconv"

	"github.com/ethosagent/meterwise/internal/contract"
)

// registerMonitors wires the Plan 06 routes. The gateway has authorized the workspace and the permission
// by the route table; every handler reads and writes through that workspace id only.
func (s *server) registerMonitors() {
	register(s, "GET", "/api/workspaces/{id}/monitors", s.getMonitors)
	register(s, "PATCH", "/api/workspaces/{id}/monitors", s.patchMonitors)
	register(s, "GET", "/api/workspaces/{id}/monitors/stats", s.monitorStats)
	register(s, "POST", "/api/workspaces/{id}/monitor-events/{eventId}/feedback", s.monitorFeedback)
}

func (s *server) getMonitors(c Call, _ none) (contract.MonitorConfig, error) {
	return s.Tenancy.MonitorConfig(c.Ctx, c.Principal, c.Path("id"))
}

func (s *server) patchMonitors(c Call, req contract.MonitorConfigPatch) (contract.MonitorConfig, error) {
	return s.Tenancy.UpdateMonitorConfig(c.Ctx, c.Principal, c.Path("id"), req)
}

func (s *server) monitorStats(c Call, _ none) (contract.MonitorStatsView, error) {
	led, err := s.ledger()
	if err != nil {
		return contract.MonitorStatsView{}, err
	}
	rng, _, from, to, err := s.statsRangeOf(c)
	if err != nil {
		return contract.MonitorStatsView{}, err
	}
	cfg, err := s.Tenancy.MonitorConfig(c.Ctx, c.Principal, c.Path("id"))
	if err != nil {
		return contract.MonitorStatsView{}, err
	}
	rows, err := led.MonitorStats(c.Ctx, c.Path("id"), from, to)
	if err != nil {
		return contract.MonitorStatsView{}, err
	}
	return monitorStatsView(rng, cfg, rows), nil
}

// monitorStatsView builds the response: the two savings numbers are summed each on its own, never together.
func monitorStatsView(rng string, cfg contract.MonitorConfig, rows []contract.MonitorStatRow) contract.MonitorStatsView {
	out := contract.MonitorStatsView{Range: rng, Monitors: make([]contract.MonitorStatsRowView, 0, len(rows))}
	for _, r := range rows {
		out.SavedUsd += r.Saved
		out.LikelySavedUsd += r.Likely
		out.WouldSaveUsd += r.WouldSave
		out.Monitors = append(out.Monitors, contract.MonitorStatsRowView{
			Monitor: r.Monitor, Mode: cfg.ModeOf(r.Monitor), Fires: r.Fires, Blocked: r.Blocked, Noted: r.Noted, Observed: r.Observed,
			SavedTokens: r.SavedTokens, LikelySavedTokens: r.LikelySavedTokens, WouldSaveTokens: r.WouldSaveTokens,
			SavedUsd: r.Saved, LikelySavedUsd: r.Likely, WouldSaveUsd: r.WouldSave,
			FeedbackCorrect: r.FeedbackCorrect, FeedbackWrong: r.FeedbackWrong,
			FalsePositiveRate: contract.FalsePositiveRate(r.FeedbackCorrect, r.FeedbackWrong),
		})
	}
	return out
}

func (s *server) monitorFeedback(c Call, req contract.MonitorFeedbackRequest) (contract.MonitorEventView, error) {
	if s.MonitorFeedback == nil {
		return contract.MonitorEventView{}, contract.Unavailable("monitor feedback", "")
	}
	if !contract.ValidFeedback(req.Feedback) {
		return contract.MonitorEventView{}, contract.Invalid("feedback", "must be \"correct\", \"wrong\" or null")
	}
	id, err := strconv.ParseInt(c.Path("eventId"), 10, 64)
	if err != nil || id <= 0 {
		return contract.MonitorEventView{}, contract.NewError(contract.CodeNotFound, "not found")
	}
	ev, err := s.MonitorFeedback.SetFeedback(c.Ctx, c.Path("id"), id, req.Feedback)
	if err != nil {
		return contract.MonitorEventView{}, err
	}
	return ev.View(), nil
}
