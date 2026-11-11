package trim

import (
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Unit is one string a stage may rewrite: a tool result (or a tool description for schema_trim) with what the stages
// may know about it. Everything in it is a function of the request's bytes and the config, never of the clock.
type Unit struct {
	Tool string
	// Seen is the 1-based message number of an earlier, byte-identical result of the same tool in this request,
	// or 0 when there is none.
	Seen int
	// MaxFoldPct is the share of a result a folding stage may fold at most (10-100).
	MaxFoldPct int
}

// Stage is one trimming step. Apply gets the running text and returns the rewritten text and true, or false when the
// stage does not apply to it. A stage never returns a longer text.
type Stage interface {
	Name() contract.TrimStage
	Apply(u Unit, text string) (string, bool)
}

// Settings are the parts of a workspace's TrimConfig the pipeline reads.
type Settings struct {
	MinGainPct int
	MaxFoldPct int
	// Enabled says which stages run; the hook has already folded the mode in (schema_trim only in aggressive).
	Enabled map[contract.TrimStage]bool
}

// StageOutcome is what one stage did to one region. Reverted means its output was discarded because the region did
// not gain MinGainPct from it.
type StageOutcome struct {
	Stage         contract.TrimStage
	Before, After int
	Reverted      bool
}

// Outcome is a region's decision: the final text and what each stage that fired did.
type Outcome struct {
	Text   string
	Stages []StageOutcome
}

// Pipeline runs the stages over one region in their fixed order.
type Pipeline struct{ stages []Stage }

// NewPipeline returns the pipeline of the four tool-result stages in their fixed order.
func NewPipeline() *Pipeline {
	return &Pipeline{stages: []Stage{repeatCollapse{}, logWindow{}, dupLines{}, jsonTable{}}}
}

// Run applies the enabled stages to text. Each stage must itself save at least MinGainPct of the region's original
// size or its output is discarded (the per-region quality gate, ruling A5); a collapsed repeat ends the run, as the
// marker is all that is left.
func (p *Pipeline) Run(u Unit, text string, s Settings) Outcome {
	out := Outcome{Text: text}
	orig := len(text)
	for _, st := range p.stages {
		if !s.Enabled[st.Name()] {
			continue
		}
		u.MaxFoldPct = s.MaxFoldPct
		next, ok := st.Apply(u, out.Text)
		if !ok || len(next) >= len(out.Text) {
			continue
		}
		so := StageOutcome{Stage: st.Name(), Before: len(out.Text), After: len(next)}
		if (len(out.Text)-len(next))*100 < s.MinGainPct*orig {
			so.Reverted, so.After = true, so.Before
			out.Stages = append(out.Stages, so)
			continue
		}
		out.Stages = append(out.Stages, so)
		out.Text = next
		if st.Name() == contract.StageRepeatCollapse {
			break
		}
	}
	return out
}

// Description runs schema_trim on one tool description. It reports whether the text was shortened.
func (p *Pipeline) Description(tool, text string, s Settings) (Outcome, bool) {
	out := Outcome{Text: text}
	if !s.Enabled[contract.StageSchemaTrim] {
		return out, false
	}
	next, ok := schemaTrim{}.Apply(Unit{Tool: tool}, text)
	if !ok || len(next) >= len(text) {
		return out, false
	}
	so := StageOutcome{Stage: contract.StageSchemaTrim, Before: len(text), After: len(next)}
	if (len(text)-len(next))*100 < s.MinGainPct*len(text) {
		return out, false
	}
	out.Stages = append(out.Stages, so)
	out.Text = next
	return out, true
}

// Budget bounds the work one call may spend on regions it has not decided before: the bytes it may analyse and the
// time it may take (the chain's deadline, less a margin). A region that does not fit is left unchanged, and stays so.
type Budget struct {
	Bytes    int
	Deadline time.Time // zero: no deadline
	Now      func() time.Time
}

// take reserves n bytes and reports whether the region may be analysed: there are bytes left and the deadline has not passed.
func (b *Budget) take(n int) bool {
	if n > b.Bytes {
		return false
	}
	if !b.Deadline.IsZero() && !b.Now().Before(b.Deadline) {
		return false
	}
	b.Bytes -= n
	return true
}
