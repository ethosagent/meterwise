package api

import (
	"context"

	"github.com/ethosagent/meterwise/internal/contract"
)

// MemoryAPI is what the control plane reads and writes for memory; the memory feature implements it and cmd wires it.
// Every call is scoped by the workspace id the gateway authorized. A nil port answers every route but the settings
// with not_available, and the settings with memory disabled for want of a key. The handler sets MemoryStats.Mode from
// the workspace's settings.
type MemoryAPI interface {
	// Status says whether memory is enabled for the workspace; when it is not, reason is one of the
	// contract.MemoryDisabled* values.
	Status(ctx context.Context, workspaceID string) (enabled bool, reason string, err error)
	Stats(ctx context.Context, workspaceID string, r contract.SavingsRange) (contract.MemoryStats, error)
	Traces(ctx context.Context, workspaceID, cursor string, limit int) (contract.TraceList, error)
	// Trace reads one trace; a trace outside the workspace is not_found.
	Trace(ctx context.Context, workspaceID, traceID string) (contract.TraceDetail, error)
	// Forget removes every trace of the workspace and says how many.
	Forget(ctx context.Context, workspaceID string) (int64, error)
	// SetHitFeedback sets or clears (nil) the feedback on a hit; a hit outside the workspace is not_found.
	SetHitFeedback(ctx context.Context, workspaceID, hitID string, feedback *string) (contract.MemoryHitView, error)
}

// registerMemory wires the memory routes. The gateway has authorized the workspace and the permission.
func (s *server) registerMemory() {
	register(s, "GET", "/api/workspaces/{id}/memory", s.getMemory)
	register(s, "PATCH", "/api/workspaces/{id}/memory", s.patchMemory)
	register(s, "GET", "/api/workspaces/{id}/memory/stats", s.memoryStats)
	register(s, "GET", "/api/workspaces/{id}/traces", s.listTraces)
	register(s, "GET", "/api/workspaces/{id}/traces/{traceId}", s.getTrace)
	register(s, "DELETE", "/api/workspaces/{id}/traces", s.forgetTraces)
	register(s, "POST", "/api/workspaces/{id}/memory-hits/{hitId}/feedback", s.memoryHitFeedback)
}

func (s *server) memoryPort() (MemoryAPI, error) {
	if s.Memory == nil {
		return nil, contract.Unavailable("memory", "")
	}
	return s.Memory, nil
}

// memorySettings is the settings response around cfg: the port's verdict, or disabled (no_key) while none is wired.
func (s *server) memorySettings(c Call, cfg contract.MemoryConfig) (contract.MemorySettings, error) {
	if s.Memory == nil {
		return contract.MemorySettings{Config: cfg, DisabledReason: new(contract.MemoryDisabledNoKey)}, nil
	}
	enabled, reason, err := s.Memory.Status(c.Ctx, c.Path("id"))
	if err != nil {
		return contract.MemorySettings{}, err
	}
	out := contract.MemorySettings{Config: cfg, Enabled: enabled}
	if !enabled && reason != "" {
		out.DisabledReason = &reason
	}
	return out, nil
}

func (s *server) getMemory(c Call, _ none) (contract.MemorySettings, error) {
	cfg, err := s.Tenancy.MemoryConfig(c.Ctx, c.Principal, c.Path("id"))
	if err != nil {
		return contract.MemorySettings{}, err
	}
	return s.memorySettings(c, cfg)
}

func (s *server) patchMemory(c Call, req contract.MemoryConfigPatch) (contract.MemorySettings, error) {
	cfg, err := s.Tenancy.UpdateMemoryConfig(c.Ctx, c.Principal, c.Path("id"), req)
	if err != nil {
		return contract.MemorySettings{}, err
	}
	out, err := s.memorySettings(c, cfg)
	return committed(out, err) // the change is stored: a failure to describe it must still be audited
}

func (s *server) memoryStats(c Call, _ none) (contract.MemoryStats, error) {
	m, err := s.memoryPort()
	if err != nil {
		return contract.MemoryStats{}, err
	}
	r, err := s.savingsRange(c)
	if err != nil {
		return contract.MemoryStats{}, err
	}
	cfg, err := s.Tenancy.MemoryConfig(c.Ctx, c.Principal, c.Path("id"))
	if err != nil {
		return contract.MemoryStats{}, err
	}
	out, err := m.Stats(c.Ctx, c.Path("id"), r)
	out.Mode = cfg.Mode
	return out, err
}

func (s *server) listTraces(c Call, _ none) (contract.TraceList, error) {
	m, err := s.memoryPort()
	if err != nil {
		return contract.TraceList{}, err
	}
	limit, err := contract.ParsePageLimit(c.Query("limit"))
	if err != nil {
		return contract.TraceList{}, err
	}
	return m.Traces(c.Ctx, c.Path("id"), c.Query("cursor"), limit)
}

func (s *server) getTrace(c Call, _ none) (contract.TraceDetail, error) {
	m, err := s.memoryPort()
	if err != nil {
		return contract.TraceDetail{}, err
	}
	return m.Trace(c.Ctx, c.Path("id"), c.Path("traceId"))
}

func (s *server) forgetTraces(c Call, _ none) (contract.ForgetResult, error) {
	m, err := s.memoryPort()
	if err != nil {
		return contract.ForgetResult{}, err
	}
	n, err := m.Forget(c.Ctx, c.Path("id"))
	return contract.ForgetResult{Count: n}, err
}

func (s *server) memoryHitFeedback(c Call, req contract.MemoryHitFeedbackRequest) (contract.MemoryHitView, error) {
	m, err := s.memoryPort()
	if err != nil {
		return contract.MemoryHitView{}, err
	}
	if !contract.ValidMemoryFeedback(req.Feedback) {
		return contract.MemoryHitView{}, contract.Invalid("feedback", "must be \"helpful\", \"irrelevant\" or null")
	}
	return m.SetHitFeedback(c.Ctx, c.Path("id"), c.Path("hitId"), req.Feedback)
}
