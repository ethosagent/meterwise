package contract

import "math/bits"

// Meta keys of the trim, memory and router features (plans 09-11). Every value is set once in the owning
// hook's Before and never mutated after; the ledger reads them in After. MetaTrim holds a TrimResult,
// MetaTrimUnfold a TrimUnfold, MetaMemory a MemoryHint and MetaRouter a RouteDecision.
const (
	MetaTrim       = "trim"
	MetaTrimUnfold = "trim.unfold"
	MetaMemory     = "memory"
	MetaRouter     = "router"
)

// TrimStage names one trimming stage. TrimStages gives the fixed order used everywhere.
type TrimStage string

// The five trimming stages.
const (
	StageRepeatCollapse TrimStage = "repeat_collapse"
	StageLogWindow      TrimStage = "log_window"
	StageDupLines       TrimStage = "dup_lines"
	StageJSONTable      TrimStage = "json_table"
	StageSchemaTrim     TrimStage = "schema_trim"
)

// TrimStages returns the five stages in their fixed order (a fresh slice).
func TrimStages() []TrimStage {
	return []TrimStage{StageRepeatCollapse, StageLogWindow, StageDupLines, StageJSONTable, StageSchemaTrim}
}

// Valid reports whether s is one of the five stages.
func (s TrimStage) Valid() bool {
	for _, v := range TrimStages() {
		if s == v {
			return true
		}
	}
	return false
}

// TrimAction says whether a trim changed the request: acted (body rewritten) or observe (would trim, body untouched).
type TrimAction string

// The two trim actions.
const (
	TrimActed   TrimAction = "acted"
	TrimObserve TrimAction = "observe"
)

// TrimStageResult is what one stage did across the call's tool results. Reverted means the stage's output was
// discarded (the region did not gain enough, R6) and the bytes were left as they were.
type TrimStageResult struct {
	Stage                   TrimStage
	Tool                    string
	Regions                 int
	BytesBefore, BytesAfter int
	Reverted                bool
}

// TrimResult is the trim hook's account of one call (Meta[MetaTrim]). BodyBefore and BodyAfter are the whole
// request body sizes in bytes; Stages has at most five entries; Unfolded counts folds undone for this call.
type TrimResult struct {
	Mode                  TrimMode
	Action                TrimAction
	BodyBefore, BodyAfter int
	Stages                []TrimStageResult
	Unfolded              int
}

// TrimUnfold (Meta[MetaTrimUnfold]) lists the ToolArgsHash of tool calls whose earlier result was folded: the
// monitors drop their duplicate_tool_call and repeated_read verdicts for those calls (the fold/unfold contract).
type TrimUnfold struct{ ArgsHashes []uint64 }

// RouteAction says what the router did: routed (model replaced), observe (would route) or held (no route).
type RouteAction string

// The three route actions.
const (
	RouteRouted  RouteAction = "routed"
	RouteObserve RouteAction = "observe"
	RouteHeld    RouteAction = "held"
)

// Reasons a route was held (RouteDecision.Reason when Action is held).
const (
	HeldFirstSteps  = "first_steps"
	HeldEscalated   = "escalated"
	HeldErrorRetry  = "error_retry"
	HeldCapability  = "capability"
	HeldNotInMap    = "not_in_map"
	HeldUnpriced    = "unpriced"
	HeldUnavailable = "unavailable"
	HeldModeOff     = "mode_off"
)

// RouteDecision is the router's account of one call (Meta[MetaRouter]). ScoreBp is the simplicity score, 0-10000.
// From is the requested model and To the target; the call's Model is To when Action is routed.
type RouteDecision struct {
	Mode    RouterMode
	Action  RouteAction
	From    string
	To      string
	ScoreBp int
	Reason  string
}

// MemoryHint is the memory hook's account of one call (Meta[MetaMemory]); used from wave 2.
type MemoryHint struct {
	Mode         MemoryMode
	TraceID      int64
	SimilarityBp int
	Injected     bool
}

// RequestFeatures is what the router needs to know about a request to check a target model's capabilities:
// the number of tool definitions and whether it asks for a JSON schema, carries images or sets reasoning.
type RequestFeatures struct {
	Tools                         int
	JSONSchema, Vision, Reasoning bool
}

// ModelCaps are a model's verified capabilities. MaxTools 0 means no tool definitions are allowed.
type ModelCaps struct {
	MaxTools                      int
	JSONSchema, Vision, Reasoning bool
}

// RouterTables is the router's read-only view of the route map, the capability table and the priced models.
// Caps is keyed by provider + "/" + model; Priced likewise.
type RouterTables struct {
	Routes []RoutePair
	Caps   map[string]ModelCaps
	Priced map[string]bool
}

// TrimTokens estimates the input tokens a body shrink saved: ceil(deltaBytes * inputTokens / sentBytes), the
// request's own measured bytes-per-token (R8). Without a usable measurement (input <= 0 or sent <= 0) it falls
// back to ceil(deltaBytes / 4). A delta <= 0 saves nothing. The product is exact (128-bit) and the result
// saturates at MaxInt64 rather than wrapping.
func TrimTokens(deltaBytes, sentBytes, inputTokens int64) int64 {
	if deltaBytes <= 0 {
		return 0
	}
	if inputTokens <= 0 || sentBytes <= 0 {
		return (deltaBytes + 3) / 4
	}
	hi, lo := bits.Mul64(uint64(deltaBytes), uint64(inputTokens))
	if hi >= uint64(sentBytes) {
		return 1<<63 - 1
	}
	q, r := bits.Div64(hi, lo, uint64(sentBytes))
	if r != 0 {
		q++
	}
	if q > 1<<63-1 {
		return 1<<63 - 1
	}
	return int64(q)
}

// ModelMatches reports whether model is the pricing-table name itself or that name plus a version
// suffix: a date (-20250514 or -2024-07-18), -latest, or a ':'/'@' qualifier. Anything else is a
// different model (claude-opus-4-5 is not claude-opus-4) and stays unpriced rather than mispriced.
func ModelMatches(name, model string) bool {
	if len(model) < len(name) || model[:len(name)] != name {
		return false
	}
	rest := model[len(name):]
	switch {
	case rest == "", rest == "-latest", rest[0] == ':', rest[0] == '@':
		return true
	}
	return isDateSuffix(rest)
}

func isDateSuffix(rest string) bool {
	if rest == "" || rest[0] != '-' {
		return false
	}
	d := rest[1:]
	switch len(d) {
	case 8:
		return allDigits(d)
	case 10:
		return allDigits(d[:4]) && d[4] == '-' && allDigits(d[5:7]) && d[7] == '-' && allDigits(d[8:])
	}
	return false
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
