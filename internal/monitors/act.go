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
	verdict int // index into the verdicts, -1 for a queued stream note
}

// decide turns fired verdicts into a plan: the first blockable one that can be replayed blocks the
// call; the rest become notes (queued stream notes first, at most MaxNotesPerCall in all, deduped
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
		h.blocks.Add(1)
		return plan{resp: resp, verdicts: fired}
	}
	items := h.collectNotes(c, fired, acts)
	body, applied := h.splice(c, items, acts)
	for i := range fired {
		fired[i].Action = acts[i]
	}
	return plan{body: body, verdicts: fired, notes: applied}
}

// tryBlock answers the call from the request's own messages. A block that cannot be built (no earlier
// identical result, or a wire error) degrades to a note and is counted.
func (h *Hook) tryBlock(c *tollgate.Call, fired []contract.MonitorVerdict, acts []contract.MonitorMode) (*tollgate.Response, int) {
	for i, v := range fired {
		if acts[i] != contract.ModeBlock {
			continue
		}
		if turn, ok := replayTurn(c.Messages, matcher(c.Tenant.ID, v)); ok {
			if resp, err := h.opt.Editor.BlockResponse(c.Provider.Wire, c.Stream, []tollgate.Message{turn}, blockNote(v)); err == nil {
				return resp, i
			}
		}
		h.blocksDegraded.Add(1)
		acts[i] = contract.ModeNote
	}
	return nil, -1
}

// collectNotes picks the notes this call carries and downgrades the verdicts that lose their note
// (over the cap, or already noted in this session) to observe in acts.
func (h *Hook) collectNotes(c *tollgate.Call, fired []contract.MonitorVerdict, acts []contract.MonitorMode) []noteItem {
	key := sessKey(c)
	var items []noteItem
	for _, t := range h.sess.take(key, contract.MaxNotesPerCall) {
		items = append(items, noteItem{t, -1})
	}
	for i, v := range fired {
		if acts[i] != contract.ModeNote {
			continue
		}
		if len(items) >= contract.MaxNotesPerCall || !h.sess.markSeen(key, dedupKey(v)) {
			acts[i] = contract.ModeObserve
			continue
		}
		items = append(items, noteItem{noteText(v), i})
	}
	return items
}

// splice adds each note to the body through the wire editor. A note that cannot be placed is dropped
// (counted) and its verdict is recorded as observed. It returns the new body (nil if unchanged) and
// the notes applied.
func (h *Hook) splice(c *tollgate.Call, items []noteItem, acts []contract.MonitorMode) ([]byte, int) {
	body, applied := c.Body, 0
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
	}
	h.notesInjected.Add(int64(applied))
	if applied == 0 {
		return nil, 0
	}
	return body, applied
}
