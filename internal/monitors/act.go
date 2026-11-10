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

var detectorOf = func() map[contract.MonitorName]Detector {
	m := map[contract.MonitorName]Detector{}
	for _, d := range Registry() {
		m[d.Name] = d
	}
	return m
}()

// effective is the action a verdict's mode allows, from its detector's registry entry: only a CanBlock
// monitor blocks in block mode (any other acts as note), and a monitor with a BlockAfter also blocks from
// that count on in note mode.
func effective(v contract.MonitorVerdict, cfg contract.MonitorConfig) contract.MonitorMode {
	d := detectorOf[v.Monitor]
	switch v.Mode {
	case contract.ModeBlock:
		if d.CanBlock {
			return contract.ModeBlock
		}
		return contract.ModeNote
	case contract.ModeNote:
		if d.BlockAfter != nil {
			if n := d.BlockAfter(cfg); n > 0 && v.Evidence.Count >= n {
				return contract.ModeBlock
			}
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
// the request, and whose subject was not blocked before in this session, blocks the call; the rest become
// notes (queued stream notes first, at most MaxNotesPerCall in all, deduped per session) or stay observed.
// The helpers below only report what they downgrade; decide alone writes the actions.
func (h *Hook) decide(c *tollgate.Call, st *callState, fired []contract.MonitorVerdict) plan {
	acts := make([]contract.MonitorMode, len(fired))
	for i, v := range fired {
		acts[i] = effective(v, st.cfg)
	}
	if resp, at := h.tryBlock(c, st.key, fired, acts); resp != nil {
		for i := range fired {
			fired[i].Action = contract.ModeObserve
		}
		fired[at].Action = contract.ModeBlock
		return plan{resp: resp, verdicts: fired, held: []noteItem{{verdict: at, key: blockKey(fired[at])}}}
	}
	for i, a := range acts {
		if a == contract.ModeBlock { // no block was possible: the verdict is noted instead
			acts[i] = contract.ModeNote
		}
	}
	items, lost := h.collectNotes(st.key, fired, acts)
	body, held, failed := h.splice(c, st.key, items)
	for _, i := range append(lost, failed...) {
		acts[i] = contract.ModeObserve
	}
	for i := range fired {
		fired[i].Action = acts[i]
	}
	return plan{body: body, verdicts: fired, notes: len(held), held: held}
}

// tryBlock answers the call itself with a terminal text turn that points at the earlier identical result
// (B1): the agent ends its turn instead of running the tool again. At most one block per session and
// subject: a later repeat of a subject already blocked degrades to a note (counted as capped). A block
// with no earlier identical result in the request, or a wire error, degrades to a note (counted). It
// returns the response and the verdict that blocked, or nil: the caller then notes every block verdict.
func (h *Hook) tryBlock(c *tollgate.Call, key string, fired []contract.MonitorVerdict, acts []contract.MonitorMode) (*tollgate.Response, int) {
	for i, v := range fired {
		if acts[i] != contract.ModeBlock {
			continue
		}
		if !h.sess.reserve(key, blockKey(v)) {
			h.blocksCapped.Add(1)
			continue
		}
		// what a block points at: the earlier identical result in the request (B1); it reads only the request
		if loc, ok := earlierResult(c.Messages, matcher(c.Tenant.ID, v)); ok {
			if resp, err := h.opt.Editor.BlockResponse(c.Provider.Wire, c.Stream, blockText(v, loc)); err == nil {
				return resp, i
			}
		}
		h.sess.release(key, blockKey(v)) // no earlier result, or the wire editor could not build the answer
		h.blocksDegraded.Add(1)
	}
	return nil, -1
}

// collectNotes picks the notes this call carries (the session's queued notes first, then the fired
// verdicts') and lists the verdicts that lose their note (over the cap, or the subject already noted or
// being noted in this session): they become observe. Each note's key is reserved in the session; splice
// delivers or gives them back.
func (h *Hook) collectNotes(key string, fired []contract.MonitorVerdict, acts []contract.MonitorMode) (items []noteItem, lost []int) {
	for _, q := range h.sess.take(key, contract.MaxNotesPerCall) {
		items = append(items, noteItem{text: q.text, verdict: -1, key: q.key})
	}
	for i, v := range fired {
		if acts[i] != contract.ModeNote {
			continue
		}
		k := dedupKey(v)
		if len(items) >= contract.MaxNotesPerCall || !h.sess.reserve(key, k) {
			lost = append(lost, i)
			continue
		}
		items = append(items, noteItem{text: noteText(v), verdict: i, key: k})
	}
	return items, lost
}

// splice adds the notes to the body through the wire editor, in one edit. If the editor cannot place them
// they are all dropped (counted): their keys are released and the queued notes go back to the session's
// queue, so only the notes spliced in are used up (K1). It returns the new body (nil if unchanged), the
// notes spliced in, and the verdicts whose note was dropped.
func (h *Hook) splice(c *tollgate.Call, key string, items []noteItem) (body []byte, held []noteItem, failed []int) {
	if len(items) == 0 {
		return nil, nil, nil
	}
	texts := make([]string, len(items))
	for i, it := range items {
		texts[i] = it.text
	}
	body, err := h.opt.Editor.AppendNotes(c.Provider.Wire, c.Body, texts)
	if err != nil {
		h.notesDropped.Add(int64(len(items)))
		for _, it := range items {
			if it.verdict >= 0 {
				failed = append(failed, it.verdict)
			}
		}
		h.giveBack(key, items)
		return nil, nil, failed
	}
	return body, items, nil
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
func (h *Hook) commit(st *callState, p plan) {
	for _, it := range p.held {
		h.sess.commit(st.key, it.key)
	}
	h.notesInjected.Add(int64(p.notes))
	if p.resp != nil {
		h.blocks.Add(1)
	}
}
