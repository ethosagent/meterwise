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
	// What Before commits to the session once it knows the plan is delivered (S2): the dedup or block keys
	// to mark as used, and the queued notes this call took (spliced or dropped).
	marks  []string
	queued []string
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
	key     string // the verdict's dedup key; "" for a queued note
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
		return plan{resp: resp, verdicts: fired, marks: []string{blockKey(fired[at])}}
	}
	items, queued := h.collectNotes(c, fired, acts)
	body, applied, marks := h.splice(c, items, acts)
	for i := range fired {
		fired[i].Action = acts[i]
	}
	return plan{body: body, verdicts: fired, notes: applied, marks: marks, queued: queued}
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
		if h.sess.seen(key, blockKey(v)) {
			h.blocksCapped.Add(1)
			continue
		}
		loc, ok := earlierResult(c.Messages, matcher(c.Tenant.ID, v))
		if !ok {
			h.blocksDegraded.Add(1)
			continue
		}
		resp, err := h.opt.Editor.BlockResponse(c.Provider.Wire, c.Stream, blockText(v, loc))
		if err != nil {
			h.blocksDegraded.Add(1)
			continue
		}
		acts[i] = contract.ModeBlock
		return resp, i
	}
	return nil, -1
}

// collectNotes picks the notes this call carries (the session's queued notes first, then the fired
// verdicts') and downgrades the verdicts that lose their note (over the cap, or already noted in this
// session) to observe in acts. It changes no session state: Before commits what was delivered.
func (h *Hook) collectNotes(c *tollgate.Call, fired []contract.MonitorVerdict, acts []contract.MonitorMode) ([]noteItem, []string) {
	key := sessKey(c)
	queued := h.sess.peek(key, contract.MaxNotesPerCall)
	items := make([]noteItem, 0, len(queued))
	for _, t := range queued {
		items = append(items, noteItem{text: t, verdict: -1})
	}
	taken := map[string]bool{}
	for i, v := range fired {
		if acts[i] != contract.ModeNote {
			continue
		}
		k := dedupKey(v)
		if len(items) >= contract.MaxNotesPerCall || taken[k] || h.sess.seen(key, k) {
			acts[i] = contract.ModeObserve
			continue
		}
		taken[k] = true
		items = append(items, noteItem{text: noteText(v), verdict: i, key: k})
	}
	return items, queued
}

// splice adds each note to the body through the wire editor. A note that cannot be placed is dropped
// (counted) and its verdict is recorded as observed; its subject stays unused. It returns the new body
// (nil if unchanged), the notes applied and the dedup keys of the verdict notes applied.
func (h *Hook) splice(c *tollgate.Call, items []noteItem, acts []contract.MonitorMode) ([]byte, int, []string) {
	body, applied := c.Body, 0
	var marks []string
	for _, it := range items {
		next, err := h.opt.Editor.AppendNote(c.Provider.Wire, body, it.text)
		if err != nil {
			h.notesDropped.Add(1)
			if it.verdict >= 0 {
				acts[it.verdict] = contract.ModeObserve
			}
			continue
		}
		body = next
		applied++
		if it.key != "" {
			marks = append(marks, it.key)
		}
	}
	if applied == 0 {
		return nil, 0, nil
	}
	return body, applied, marks
}

// commit records a delivered plan in the session: the subjects it noted or blocked are used up, and
// the queued notes it took leave the queue. Before calls it last, once the chain still waits (S2).
func (h *Hook) commit(c *tollgate.Call, p plan) {
	key := sessKey(c)
	for _, k := range p.marks {
		h.sess.markSeen(key, k)
	}
	h.sess.drop(key, p.queued)
	h.notesInjected.Add(int64(p.notes))
	if p.resp != nil {
		h.blocks.Add(1)
	}
}
