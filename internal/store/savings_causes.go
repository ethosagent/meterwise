package store

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store/sqlc"
)

// Row caps of the causes read: clients shown and repeat targets returned.
const (
	maxClients       = 50
	maxRepeatTargets = 20
	unknownClient    = "unknown"
)

// Causes reads "where the savings came from" for a validated range: the six per-monitor causes (Mode and
// Confidence for the caller to fill, as in Overview), the per-client table and the repeat targets the caller
// turns into suggestions. All reads share one snapshot. A client's previous-window average is null when the
// client had no calls in the same-length window before the range.
func (s *Store) Causes(ctx context.Context, workspaceID string, r contract.SavingsRange) (contract.SavingsCauses, []contract.RepeatTarget, error) {
	if !validID(workspaceID) {
		return contract.SavingsCauses{}, nil, contract.Invalid("workspaceId", "must be a workspace id")
	}
	from, to := window(r)
	prevFrom := from.AddDate(0, 0, -r.Days())
	var (
		stats   []contract.MonitorStatRow
		tops    []sqlc.SavingsCauseTopsRow
		repeats []sqlc.SavingsRepeatTargetsRow
		cur     []sqlc.SavingsClientsRow
		saved   []sqlc.SavingsClientSavedRow
	)
	err := s.readSnapshot(ctx, func(q *sqlc.Queries) (err error) {
		if stats, err = s.monitorStatsWith(ctx, q, workspaceID, from, to); err != nil {
			return err
		}
		if tops, err = q.SavingsCauseTops(ctx, sqlc.SavingsCauseTopsParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)}); err != nil {
			return err
		}
		if repeats, err = q.SavingsRepeatTargets(ctx, sqlc.SavingsRepeatTargetsParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to), MaxRows: maxRepeatTargets}); err != nil {
			return err
		}
		if cur, err = q.SavingsClients(ctx, sqlc.SavingsClientsParams{WorkspaceID: workspaceID, PrevFromTs: pgts(prevFrom), FromTs: pgts(from), ToTs: pgts(to), MaxRows: maxClients}); err != nil {
			return err
		}
		saved, err = q.SavingsClientSaved(ctx, sqlc.SavingsClientSavedParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to)})
		return err
	})
	if err != nil {
		return contract.SavingsCauses{}, nil, err
	}
	out := contract.SavingsCauses{Range: r.Range, From: r.FromDay(), To: r.ToDay(), Timezone: "UTC", Causes: causesOf(stats, tops), Clients: clientRows(cur, saved)}
	targets := make([]contract.RepeatTarget, len(repeats))
	for i, t := range repeats {
		targets[i] = contract.RepeatTarget{Monitor: contract.MonitorName(t.Monitor), Tool: t.Tool, Path: t.Path, PathHash: t.PathHash, Count: t.N}
	}
	return out, targets, nil
}

// clientRows joins the current window's calls (already busiest first, with the previous window's counts) with the saved money. NULL clients read "unknown".
func clientRows(cur []sqlc.SavingsClientsRow, saved []sqlc.SavingsClientSavedRow) []contract.ClientRow {
	sv := make(map[string]sqlc.SavingsClientSavedRow, len(saved))
	for _, s := range saved {
		sv[s.Client] = s
	}
	out := make([]contract.ClientRow, 0, len(cur))
	for _, c := range cur {
		row := contract.ClientRow{
			Client: c.Client, Calls: c.Calls, Sessions: c.Sessions, Blocked: c.Blocked, SpentUsd: contract.MicroUSD(c.SpentMicro),
			SavedUsd: contract.MicroUSD(sv[c.Client].SavedMicro), LikelySavedUsd: contract.MicroUSD(sv[c.Client].LikelyMicro),
		}
		if row.Client == "" {
			row.Client = unknownClient
		}
		row.SavedBp = basisPoints(row.SavedUsd, row.SpentUsd+row.SavedUsd)
		if c.Sessions > 0 {
			row.AvgCallsPerSession = float64(c.Calls) / float64(c.Sessions)
		}
		if c.PrevSessions > 0 {
			row.PrevAvgCallsPerSession = new(float64(c.PrevCalls) / float64(c.PrevSessions))
		}
		out = append(out, row)
	}
	return out
}

// Saves reads one page of the biggest saves of a range: acted events with a positive estimate, both
// confidences, (estimate DESC, id DESC) behind an opaque keyset cursor. limit follows the shared page bounds (contract.ResolvePageLimit).
func (s *Store) Saves(ctx context.Context, workspaceID string, r contract.SavingsRange, cursor string, limit int) (contract.SaveList, error) {
	if !validID(workspaceID) {
		return contract.SaveList{}, contract.Invalid("workspaceId", "must be a workspace id")
	}
	limit, err := contract.ResolvePageLimit(limit)
	if err != nil {
		return contract.SaveList{}, err
	}
	micro, id, err := decodeSaveCursor(cursor)
	if err != nil {
		return contract.SaveList{}, err
	}
	from, to := window(r)
	p := sqlc.ListSavesParams{WorkspaceID: workspaceID, FromTs: pgts(from), ToTs: pgts(to), MaxRows: clampInt32(int64(limit + 1))}
	if cursor != "" {
		p.CursorMicro, p.CursorID = pgtype.Int8{Int64: micro, Valid: true}, pgtype.Int8{Int64: id, Valid: true}
	}
	rows, err := s.q.ListSaves(ctx, p)
	if err != nil {
		return contract.SaveList{}, queryErr(err)
	}
	out := contract.SaveList{Items: make([]contract.SaveRow, 0, min(len(rows), limit))}
	for i, e := range rows {
		if i == limit { // the extra row only proves there is a next page
			last := rows[limit-1]
			out.NextCursor = new(encodeSaveCursor(last.EstMicro, last.ID))
			break
		}
		out.Items = append(out.Items, contract.SaveRow{
			EventID: strconv.FormatInt(e.ID, 10), SessionID: e.SessionID, CallID: e.CallID, CallIndex: int(e.CallIndex), TS: rfc3339(e.Ts),
			Monitor: contract.MonitorName(e.Monitor), Action: contract.MonitorMode(e.Action), Confidence: contract.Confidence(e.Confidence),
			EstSavedUsd: contract.MicroUSD(e.EstMicro), EstSavedTokens: e.EstSavedTokens, Client: textPtr(e.Client), Model: e.Model,
		})
	}
	return out, nil
}

// The saves cursor reuses the keyset codec: the "time" is the estimate in micro-USD and the id is the event id.
func encodeSaveCursor(micro, id int64) string {
	return encodeCursor(time.UnixMicro(micro), strconv.FormatInt(id, 10))
}

func decodeSaveCursor(c string) (micro, id int64, err error) {
	at, rawID, err := decodeCursor(c)
	if err != nil || c == "" {
		return 0, 0, err
	}
	id, perr := strconv.ParseInt(strings.TrimSpace(rawID), 10, 64)
	if perr != nil || id <= 0 || at.UnixMicro() <= 0 {
		return 0, 0, contract.Invalid("cursor", "is not a cursor this API issued")
	}
	return at.UnixMicro(), id, nil
}
