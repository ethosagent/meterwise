package store

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/ethosagent/meterwise/internal/contract"
)

// efSearch is the HNSW candidate list size of the recall query: the query wants 3 neighbours and iterative scans cover
// the workspace filter, so a list of 20 is enough (the pgvector default is 40; ADR 0031 has the measurements).
const efSearch = 20

// tuneRecallSQL sets the session-local pgvector knobs of the recall query. They last to the end of the batch's
// implicit transaction. plan_cache_mode keeps the prepared statement on a custom plan: a generic plan cannot see the
// constant vector and picks a sequential scan (10 s at 100 000 traces).
const tuneRecallSQL = `SELECT set_config('hnsw.iterative_scan', 'relaxed_order', true), set_config('hnsw.ef_search', $1::text, true),
       set_config('plan_cache_mode', 'force_custom_plan', true)`

// nearestSQL is the recall query: the traces of ONE workspace closest to the vector by cosine distance, at or above
// the minimum confidence; similarity is 1 - distance (computed at full precision for the rows returned; the ORDER BY uses
// the halfvec expression the HNSW index is on). It is written here, not in queries/, so it can travel in one
// batch with the tuning statement (one round trip instead of four); the workspace scope is in the statement itself.
const nearestSQL = `SELECT id, session_id::text, approach, fix, signature,
       (1 - (embedding::vector(1536) <=> $1::text::vector(1536)))::float8
FROM memory_traces
WHERE workspace_id = $2::text::uuid AND confidence >= $3::real
ORDER BY embedding::halfvec(1536) <=> $1::text::halfvec(1536)
LIMIT $4::int`

// NearestTraces returns the traces of ONE workspace closest to vec by cosine distance whose confidence is at least
// minConf, at most k, nearest first. The workspace filter is part of the query (no cross-workspace recall, ever).
func (s *Store) NearestTraces(ctx context.Context, workspaceID string, vec []float32, k int, minConf float32) ([]Candidate, error) {
	if !validID(workspaceID) {
		return nil, contract.Invalid("workspaceId", "must be a workspace id")
	}
	lit, err := vectorLiteral(vec)
	if err != nil {
		return nil, err
	}
	limit := clampInt32(int64(k))
	out, err := s.nearest(ctx, true, lit, workspaceID, minConf, limit)
	if err != nil && ctx.Err() == nil { // a pgvector older than 0.8 has no iterative scan: query untuned
		out, err = s.nearest(ctx, false, lit, workspaceID, minConf, limit)
	}
	return out, queryErr(err)
}

func (s *Store) nearest(ctx context.Context, tuned bool, lit, ws string, minConf float32, k int32) ([]Candidate, error) {
	var b pgx.Batch
	if tuned {
		b.Queue(tuneRecallSQL, strconv.Itoa(efSearch))
	}
	b.Queue(nearestSQL, lit, ws, minConf, k)
	br := s.pool.SendBatch(ctx, &b)
	defer br.Close()
	if tuned {
		if _, err := br.Exec(); err != nil {
			return nil, err
		}
	}
	rows, err := br.Query()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var c Candidate
		var sig []byte
		if err := rows.Scan(&c.ID, &c.SessionID, &c.Approach, &c.Fix, &sig, &c.Similarity); err != nil {
			return nil, err
		}
		c.Signature = signatureOf(sig)
		out = append(out, c)
	}
	return out, rows.Err()
}
