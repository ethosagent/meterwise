package trim

import (
	"cmp"
	"context"
	"hash/maphash"
	"slices"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/wire"
)

var hashSeed = maphash.MakeSeed()

// hashOf hashes a region: the tool that produced it and its bytes as they are in the body.
func hashOf(tool string, raw []byte) uint64 {
	var h maphash.Hash
	h.SetSeed(hashSeed)
	_, _ = h.WriteString(tool)
	_ = h.WriteByte(0)
	_, _ = h.Write(raw)
	return h.Sum64()
}

// firstSeen is where a content hash first appeared in this request: the 1-based message number, and how many copies
// have been met.
type firstSeen struct{ msg, copies int }

// stageAgg totals one stage over the call's regions.
type stageAgg struct {
	regions, before, after int
	reverted, revBytes     int
	tool                   string
	best                   int
}

// callRun is one Before: the request, the config, the session's memo and what has been decided so far.
type callRun struct {
	h      *Hook
	c      *tollgate.Call
	cfg    cfgEntry
	acting bool
	sm     *sessMemo
	budget Budget
	seen   map[uint64]firstSeen

	edits    []wire.Edit
	saved    int
	agg      [5]stageAgg
	unfolded []uint64
}

func (h *Hook) newRun(ctx context.Context, c *tollgate.Call, cfg cfgEntry) *callRun {
	now := h.opt.Now()
	dl := now.Add(h.opt.MaxWork)
	if d, ok := ctx.Deadline(); ok && d.Add(-h.opt.MinDeadline).Before(dl) {
		dl = d.Add(-h.opt.MinDeadline)
	}
	return &callRun{
		h: h, c: c, cfg: cfg, acting: cfg.cfg.Mode != contract.TrimModeObserve,
		sm:     h.memo.get(sessKey(c.Tenant.ID, c.Session.ID)),
		budget: Budget{Bytes: h.opt.CallBytes, Deadline: dl, Now: h.opt.Now},
		seen:   map[uint64]firstSeen{},
	}
}

// execute decides every region, splices the acted ones into the body and reports the result in Meta.
func (r *callRun) execute() {
	c := r.c
	before := len(c.Body)
	regs, err := wire.ToolResultSpans(c.Provider.Wire, c.Body)
	if err != nil {
		r.h.editFailed.Add(1)
		return
	}
	var descs []wire.Region
	if r.cfg.st.Enabled[contract.StageSchemaTrim] {
		if descs, err = wire.ToolDescriptions(c.Provider.Wire, c.Body); err != nil {
			r.h.editFailed.Add(1)
			return
		}
	}
	if r.acting {
		r.unfold()
	}
	for _, g := range regs {
		r.region(g, false)
	}
	for _, g := range descs {
		r.region(g, true)
	}
	if r.acting && len(r.edits) > 0 {
		slices.SortFunc(r.edits, func(a, b wire.Edit) int { return cmp.Compare(a.Start, b.Start) })
		nb, err := wire.ReplaceRegions(c.Body, r.edits)
		if err != nil {
			r.h.editFailed.Add(1)
			return
		}
		c.Body = nb
	}
	r.report(before)
}

// report stamps Meta with what the call did, when it did anything.
func (r *callRun) report(before int) {
	if r.saved == 0 && len(r.unfolded) == 0 {
		return
	}
	res := contract.TrimResult{Mode: r.cfg.cfg.Mode, Action: contract.TrimObserve, BodyBefore: before, BodyAfter: before - r.saved,
		Stages: r.stageResults(), Unfolded: len(r.unfolded)}
	if r.acting {
		res.Action = contract.TrimActed
		res.BodyAfter = len(r.c.Body)
		r.h.acted.Add(1)
	} else {
		r.h.observed.Add(1)
	}
	if r.c.Meta == nil {
		r.c.Meta = map[string]any{}
	}
	r.c.Meta[contract.MetaTrim] = res
	if len(r.unfolded) > 0 {
		r.c.Meta[contract.MetaTrimUnfold] = contract.TrimUnfold{ArgsHashes: r.unfolded}
	}
}

func (r *callRun) stageResults() []contract.TrimStageResult {
	var out []contract.TrimStageResult
	for i, s := range contract.TrimStages() {
		a := r.agg[i]
		switch {
		case a.regions > 0:
			out = append(out, contract.TrimStageResult{Stage: s, Tool: a.tool, Regions: a.regions, BytesBefore: a.before, BytesAfter: a.after})
		case a.reverted > 0:
			out = append(out, contract.TrimStageResult{Stage: s, Regions: a.reverted, BytesBefore: a.revBytes, BytesAfter: a.revBytes, Reverted: true})
		}
	}
	return out
}

// unfold applies the fold/unfold contract: when the newest assistant turn repeats a tool call whose result was
// folded, that result is sent whole from now on, and the monitors are told the repeat is not waste (Meta[trim.unfold]).
func (r *callRun) unfold() {
	msgs := r.c.Messages
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != tollgate.RoleAssistant {
			continue
		}
		newest := make([]string, len(msgs[i].ToolCalls))
		for k, tc := range msgs[i].ToolCalls {
			newest[k] = tc.ID
		}
		for _, tc := range msgs[i].ToolCalls {
			if h := contract.ToolArgsHash(tc.Name, tc.Arguments); r.sm.unfold(h, newest) {
				r.unfolded = append(r.unfolded, h)
				r.h.unfolds.Add(1)
			}
		}
		return
	}
}

// region decides one string (a tool result, or a tool description when desc) and records what it contributes.
func (r *callRun) region(g wire.Region, desc bool) {
	raw := r.c.Body[g.Start:g.End]
	if len(raw) < minRegion || g.Protected {
		return
	}
	tool := g.Tool
	if desc {
		tool = "\x00desc\x00" + tool
	}
	h := hashOf(tool, raw)
	fs := r.seen[h]
	key := memoKey{h, fs.copies}
	if fs.copies == 0 {
		fs.msg = g.Msg + 1
	}
	fs.copies++
	r.seen[h] = fs
	d, ok := r.sm.get(key)
	fresh := !ok
	if fresh {
		seenMsg := 0
		if key.k > 0 {
			seenMsg = fs.msg
		}
		var dropped int
		d, dropped = r.sm.put(key, r.decide(g, raw, seenMsg, desc))
		r.h.memo.evicted.Add(int64(dropped))
	}
	r.use(g, d, fresh)
}

// decide runs the pipeline on a region the session has not decided before.
func (r *callRun) decide(g wire.Region, raw []byte, seenMsg int, desc bool) decision {
	d := decision{tool: g.Tool}
	d.args, d.hasArgs = argsOf(r.c.Messages, g.ToolCallID)
	d.callID = g.ToolCallID
	switch {
	case d.hasArgs && r.sm.isUnfolded(d.args), r.cfg.never(g.Tool):
		return d
	case len(raw) > maxRegion || !r.budget.take(len(raw)):
		r.h.deadlineSkips.Add(1)
		return d
	}
	text := g.Decode(r.c.Body)
	var out Outcome
	if desc {
		out, _ = r.h.pipe.Description(g.Tool, text, r.cfg.st)
	} else {
		out = r.h.pipe.Run(Unit{Tool: g.Tool, Seen: seenMsg}, text, r.cfg.st)
	}
	d.stages = out.Stages
	if !fired(out.Stages) {
		return d
	}
	if saved := len(raw) - jsonLen(out.Text); saved > 0 {
		d.saved = saved
		if r.acting {
			d.out, d.applied = out.Text, true
		}
	}
	return d
}

func fired(ss []StageOutcome) bool {
	for _, s := range ss {
		if !s.Reverted {
			return true
		}
	}
	return false
}

// argsOf finds the tool call with this id in the request's assistant turns and returns its ToolArgsHash.
func argsOf(msgs []tollgate.Message, id string) (uint64, bool) {
	if id == "" {
		return 0, false
	}
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			if tc.ID == id {
				return contract.ToolArgsHash(tc.Name, tc.Arguments), true
			}
		}
	}
	return 0, false
}

// use adds a decision to the call: an edit and its stage totals when acting, the would-save when observing. A stage
// that was reverted is reported only by the call that decided it.
func (r *callRun) use(g wire.Region, d decision, fresh bool) {
	counted := d.saved > 0 && (!r.acting || d.applied)
	if counted {
		r.saved += d.saved
		if r.acting {
			r.edits = append(r.edits, wire.Edit{Start: g.Start, End: g.End, Text: d.out})
		}
	}
	for _, s := range d.stages {
		a := &r.agg[stageIndex(s.Stage)]
		switch {
		case s.Reverted:
			if fresh {
				a.reverted++
				a.revBytes += s.Before
			}
		case counted:
			a.regions++
			a.before += s.Before
			a.after += s.After
			if gain := s.Before - s.After; gain > a.best {
				a.best, a.tool = gain, d.tool
			}
		}
	}
}

func stageIndex(s contract.TrimStage) int {
	for i, v := range contract.TrimStages() {
		if v == s {
			return i
		}
	}
	return 0
}
