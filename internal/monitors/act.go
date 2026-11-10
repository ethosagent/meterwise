package monitors

import (
	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// plan is what Before does about the fired verdicts: answer the call itself (resp), or send body
// (when notes were spliced in) upstream, and which verdicts to record.
type plan struct {
	resp     *tollgate.Response
	body     []byte
	verdicts []contract.MonitorVerdict
	notes    int
	// What the plan holds in the session until Before knows it is delivered (S2): the dedup or block keys
	// it reserved and the queued notes it took, for the notes spliced and the block built. Before commits
	// them once the chain still waits, and aborts them (keys released, queued notes put back) otherwise.
	held []noteItem
}

// effective is the action a verdict's mode allows. Only duplicate_tool_call and repeated_read can
// block; repeated_read in note mode blocks from its BlockAfter-th read on; every other monitor's
// block mode acts as note.
func effective(v contract.MonitorVerdict, cfg contract.MonitorConfig) contract.MonitorMode {
	canBlock := v.Monitor == contract.MonitorDuplicateToolCall || v.Monitor == contract.MonitorRepeatedRead
	switch v.Mode {
	case contract.ModeBlock:
		if canBlock {
			return contract.ModeBlock
		}
		return contract.ModeNote
	case contract.ModeNote:
		if v.Monitor == contract.MonitorRepeatedRead && cfg.RepeatedRead.BlockAfter > 0 && v.Evidence.Count >= cfg.RepeatedRead.BlockAfter {
			return contract.ModeBlock
		}
		return contract.ModeNote
	}
	return contract.ModeObserve
}

type noteItem struct {
	text    string
	verdict int    // index into the verdicts, -1 for a queued stream note
	key     string // the dedup key it reserved
}

// decide turns fired verdicts into a plan: the first blockable one whose earlier identical result is in
// the request, and whose subject was not blocked before in this session, blocks the call; the rest become notes (queued stream notes first, at most MaxNotesPerCall in all, deduped
// per session) or stay observed.
func (h *Hook) decide(c *tollgate.Call, fired []contract.MonitorVerdict, cfg contract.MonitorConfig) plan {
	acts := make([]contract.MonitorMode, len(fired))
	for i, v := range fired {
		acts[i] = effective(v, cfg)
	}
	if resp, at := h.tryBlock(c, fired, acts); resp != nil {
		for i := range fired {
			fired[i].Action = contract.ModeObserve
		}
		fired[at].Action = contract.ModeBlock
		return plan{resp: resp, verdicts: fired, held: []noteItem{{verdict: at, key: blockKey(fired[at])}}}
	}
	body, held := h.splice(c, h.collectNotes(c, fired, acts), acts)
	for i := range fired {
		fired[i].Action = acts[i]
	}
	return plan{body: body, verdicts: fired, notes: len(held), held: held}
}

// tryBlock answers the call itself with a terminal text turn that points at the earlier identical result
// (B1): the agent ends its turn instead of running the tool again. At most one block per session and
// subject: a later repeat of a subject already blocked degrades to a note (counted as capped). A block
// with no earlier identical result in the request, or a wire error, degrades to a note (counted).
func (h *Hook) tryBlock(c *tollgate.Call, fired []contract.MonitorVerdict, acts []contract.MonitorMode) (*tollgate.Response, int) {
	key := sessKey(c)
	for i, v := range fired {
		if acts[i] != contract.ModeBlock {
			continue
		}
		acts[i] = contract.ModeNote
		if !h.sess.reserve(key, blockKey(v)) {
			h.blocksCapped.Add(1)
			continue
		}
		loc, ok := earlierResult(c.Messages, matcher(c.Tenant.ID, v))
		if !ok {
			h.sess.release(key, blockKey(v))
			h.blocksDegraded.Add(1)
			continue
		}
		resp, err := h.opt.Editor.BlockResponse(c.Provider.Wire, c.Stream, blockText(v, loc))
		if err != nil {
			h.sess.release(key, blockKey(v))
			h.blocksDegraded.Add(1)
			continue
		}
		acts[i] = contract.ModeBlock
		return resp, i
	}
	return nil, -1
}

// collectNotes picks the notes this call carries (the session's queued notes first, then the fired
// verdicts') and downgrades the verdicts that lose their note (over the cap, or the subject already noted
// or being noted in this session) to observe in acts. Each note's key is reserved in the session; splice
// delivers or gives them back.
func (h *Hook) collectNotes(c *tollgate.Call, fired []contract.MonitorVerdict, acts []contract.MonitorMode) []noteItem {
	key := sessKey(c)
	var items []noteItem
	for _, q := range h.sess.take(key, contract.MaxNotesPerCall) {
		items = append(items, noteItem{text: q.text, verdict: -1, key: q.key})
	}
	for i, v := range fired {
		if acts[i] != contract.ModeNote {
			continue
		}
		k := dedupKey(v)
		if len(items) >= contract.MaxNotesPerCall || !h.sess.reserve(key, k) {
			acts[i] = contract.ModeObserve
			continue
		}
		items = append(items, noteItem{text: noteText(v), verdict: i, key: k})
	}
	return items
}

// splice adds each note to the body through the wire editor. A note that cannot be placed is dropped
// (counted): its verdict is recorded as observed and its key released, and a queued note goes back to the
// session's queue, so only the notes spliced in are used up (K1). It returns the new body (nil if
// unchanged) and the notes spliced in.
func (h *Hook) splice(c *tollgate.Call, items []noteItem, acts []contract.MonitorMode) ([]byte, []noteItem) {
	body := c.Body
	var held, undone []noteItem
	for _, it := range items {
		next, err := h.opt.Editor.AppendNote(c.Provider.Wire, body, it.text)
		if err != nil {
			h.notesDropped.Add(1)
			undone = append(undone, it)
			if it.verdict >= 0 {
				acts[it.verdict] = contract.ModeObserve
			}
			continue
		}
		body = next
		held = append(held, it)
	}
	h.giveBack(sessKey(c), undone)
	if len(held) == 0 {
		return nil, nil
	}
	return body, held
}

// giveBack returns what a plan held but did not deliver to the session: a verdict's key is released, a
// queued note goes back to the front of the queue (its key stays reserved).
func (h *Hook) giveBack(key string, items []noteItem) {
	var queued []queuedNote
	for _, it := range items {
		if it.verdict >= 0 {
			h.sess.release(key, it.key)
		} else {
			queued = append(queued, queuedNote{it.key, it.text})
		}
	}
	h.sess.restore(key, queued)
}

// commit records a delivered plan in the session: the subjects it noted or blocked, and the queued notes
// it spliced, are used up. Before calls it last, once the chain still waits (S2).
func (h *Hook) commit(c *tollgate.Call, p plan) {
	key := sessKey(c)
	for _, it := range p.held {
		h.sess.commit(key, it.key)
	}
	h.notesInjected.Add(int64(p.notes))
	if p.resp != nil {
		h.blocks.Add(1)
	}
}
