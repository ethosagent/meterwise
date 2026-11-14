package memory

import (
	"context"
	"sort"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

const (
	recallK       = 3
	recallMinConf = 0.6
)

// recallInput is what a recall job needs, copied out of the session so the job holds no lock.
type recallInput struct {
	ws, id string
	cfg    contract.MemoryConfig
	text   string
	used   map[int64]struct{}
}

func (s *sessions) evictions() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.evicts
}

// runRecall is the off-path half of recall (ruling R1): embed the task, ask pgvector for the nearest traces of this
// workspace, keep the best one over the workspace's threshold and cache its rendered hint for the session's next
// Before. Any failure leaves the session without a candidate; it never reaches a request.
func (e *Engine) runRecall(ctx context.Context, in recallInput) error {
	e.recalls.Add(1)
	cand, err := e.recallOnce(ctx, in)
	e.sess.setCandidate(in.ws, in.id, cand)
	if cand != nil {
		e.recallHits.Add(1)
	} else if err == nil {
		e.sess.retryRecall(in.ws, in.id) // nothing close enough yet: a later error may change the text
	}
	return err
}

func (e *Engine) recallOnce(ctx context.Context, in recallInput) (*candidate, error) {
	if in.text == "" {
		return nil, nil
	}
	if e.overBudget(ctx, in.ws) {
		e.skippedBudget.Add(1)
		return nil, nil
	}
	model := e.embedModel(in.cfg)
	vec, u, err := e.opt.Embedder.Embed(ctx, model, in.text)
	e.recordUsage(ctx, in.ws, "embed", model, u)
	if err != nil {
		return nil, err
	}
	found, err := e.opt.Store.NearestTraces(ctx, in.ws, vec, recallK, recallMinConf)
	if err != nil {
		return nil, err
	}
	return pick(found, in), nil
}

// pick chooses the best candidate at or over the threshold that the session has not already been given.
func pick(found []store.Candidate, in recallInput) *candidate {
	floor := float64(in.cfg.ThresholdBp) / 10000
	ok := found[:0:0]
	for _, c := range found {
		if _, used := in.used[c.ID]; !used && c.Similarity >= floor {
			ok = append(ok, c)
		}
	}
	if len(ok) == 0 {
		return nil
	}
	sort.SliceStable(ok, func(i, j int) bool { return ok[i].Similarity > ok[j].Similarity })
	b := ok[0]
	return &candidate{traceID: b.ID, simBp: int(b.Similarity*10000 + 0.5), hint: RenderHint(b.Approach, b.Fix, b.Signature.Files)}
}
