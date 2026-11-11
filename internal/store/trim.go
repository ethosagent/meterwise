package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// maxTrimTools caps the by-tool table of the trim stats (contract.TrimStats.ByTool: at most 20 tools).
const maxTrimTools = 20

// trimMarkers are the fixed templates the preview shows for a stage, never request text (privacy mode: the preview
// holds sizes and markers only). They mirror what internal/trim writes into a request; no body is stored.
var trimMarkers = map[contract.TrimStage]string{
	contract.StageRepeatCollapse: "[unchanged since step N — see above]",
	contract.StageLogWindow:      "… N lines folded; K errors kept …",
	contract.StageDupLines:       "line (×N)",
	contract.StageJSONTable:      "[N]{columns}:",
	contract.StageSchemaTrim:     "description … (shortened)",
}

// TrimReader is the read port behind GET /trim/stats and GET /calls/{id}/trim. It reads the calls and trim_events
// the ledger wrote; every query is scoped by the workspace the gateway authorized.
type TrimReader struct{ s *Store }

// NewTrimReader returns the trim read port over s.
func NewTrimReader(s *Store) *TrimReader { return &TrimReader{s: s} }

// Stats reads trim's totals, per-stage and per-tool tables over a validated range. All reads share one snapshot,
// so the totals, the stage table and the tool table agree even while calls land. The three savings classes are
// separate fields (ADR 0023). Mode is left for the caller (the API handler fills it from the workspace settings).
func (t *TrimReader) Stats(ctx context.Context, workspaceID string, r contract.SavingsRange) (contract.TrimStats, error) {
	if !validID(workspaceID) {
		return contract.TrimStats{}, contract.Invalid("workspaceId", "must be a workspace id")
	}
	from, to := window(r)
	var (
		totals sqlc.TrimCallStatsRow
		stages []sqlc.TrimStageStatsRow
		tools  []sqlc.TrimToolStatsRow
	)
	err := t.s.readSnapshot(ctx, func(q *sqlc.Queries) (err error) {
		if totals, err = q.TrimCallStats(ctx, sqlc.TrimCallStatsParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)}); err != nil {
			return err
		}
		if stages, err = q.TrimStageStats(ctx, sqlc.TrimStageStatsParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)}); err != nil {
			return err
		}
		tools, err = q.TrimToolStats(ctx, sqlc.TrimToolStatsParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to), MaxRows: maxTrimTools})
		return err
	})
	if err != nil {
		return contract.TrimStats{}, err
	}
	return contract.TrimStats{
		Range: r.Range, From: r.FromDay(), To: r.ToDay(),
		Calls: totals.Calls, TrimmedCalls: totals.TrimmedCalls,
		SavedUsd: contract.MicroUSD(totals.SavedMicro), LikelySavedUsd: contract.MicroUSD(totals.LikelyMicro), WouldSaveUsd: contract.MicroUSD(totals.WouldMicro),
		TrimTokens: totals.TrimTokens, WouldTrimTokens: totals.WouldTokens,
		ByStage: stageRows(stages), ByTool: toolRows(tools),
	}, nil
}

// stageRows lists the five stages in their fixed order, zero where a stage never fired.
func stageRows(rows []sqlc.TrimStageStatsRow) []contract.TrimStageStat {
	by := make(map[string]sqlc.TrimStageStatsRow, len(rows))
	for _, r := range rows {
		by[r.Stage] = r
	}
	out := make([]contract.TrimStageStat, 0, 5)
	for _, st := range contract.TrimStages() {
		r := by[string(st)]
		out = append(out, contract.TrimStageStat{Stage: st, Fires: r.Fires, Reverted: r.Reverted, Tokens: r.Tokens, BytesSaved: r.BytesSaved})
	}
	return out
}

func toolRows(rows []sqlc.TrimToolStatsRow) []contract.TrimToolStat {
	out := make([]contract.TrimToolStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, contract.TrimToolStat{Tool: r.Tool, Fires: r.Fires, Tokens: r.Tokens})
	}
	return out
}

// Preview reads what trimming did to one call: its action and, per stage, sizes, the token estimate and our fixed
// marker template. No request text exists to read. BodyBefore and BodyAfter are the bytes of the trimmed regions
// summed over the stage rows (a stage's before is the previous stage's after, so a region seen by two stages counts
// twice in each); the whole request size is not stored. A call that is not the workspace's is not found; a call that
// was never trimmed has no action and no stages.
func (t *TrimReader) Preview(ctx context.Context, workspaceID, callID string) (contract.TrimPreview, error) {
	if !validID(workspaceID) {
		return contract.TrimPreview{}, contract.Invalid("workspaceId", "must be a workspace id")
	}
	var (
		action pgtype.Text
		events []sqlc.TrimCallEventsRow
	)
	err := t.s.readSnapshot(ctx, func(q *sqlc.Queries) (err error) {
		if action, err = q.TrimCallAction(ctx, sqlc.TrimCallActionParams{WorkspaceID: workspaceID, CallID: callID}); err != nil {
			return err
		}
		events, err = q.TrimCallEvents(ctx, sqlc.TrimCallEventsParams{WorkspaceID: workspaceID, CallID: callID})
		return err
	})
	if err != nil {
		return contract.TrimPreview{}, err
	}
	out := contract.TrimPreview{CallID: callID, Stages: make([]contract.TrimStageView, 0, len(events))}
	if action.Valid {
		a := contract.TrimAction(action.String)
		out.Action = &a
	}
	for _, e := range events {
		v := contract.TrimStageView{
			Stage: contract.TrimStage(e.Stage), Regions: int(e.Regions), BytesBefore: int(e.BytesBefore), BytesAfter: int(e.BytesAfter),
			EstTokens: e.EstTokens, Reverted: e.Reverted, Marker: trimMarkers[contract.TrimStage(e.Stage)],
		}
		if e.ToolName.Valid && e.ToolName.String != "" {
			v.Tool = &e.ToolName.String
		}
		out.BodyBefore += v.BytesBefore
		out.BodyAfter += v.BytesAfter
		out.Stages = append(out.Stages, v)
	}
	return out, nil
}
