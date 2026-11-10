package contract

import (
	"context"
	"sync"
	"time"
)

// MonitorName names one waste detector. MonitorNames gives the fixed order used everywhere.
type MonitorName string

// The six monitors.
const (
	MonitorDuplicateToolCall MonitorName = "duplicate_tool_call"
	MonitorRepeatedRead      MonitorName = "repeated_read"
	MonitorNoProgress        MonitorName = "no_progress"
	MonitorStepBudget        MonitorName = "step_budget"
	MonitorReasoningLoop     MonitorName = "reasoning_loop"
	MonitorSecondGuessing    MonitorName = "second_guessing"
)

// MonitorNames returns the six monitors in their fixed order (a fresh slice).
func MonitorNames() []MonitorName {
	return []MonitorName{MonitorDuplicateToolCall, MonitorRepeatedRead, MonitorNoProgress,
		MonitorStepBudget, MonitorReasoningLoop, MonitorSecondGuessing}
}

// MonitorMode is what a monitor may do: observe (record only), note (add a note) or block.
type MonitorMode string

// The three modes.
const (
	ModeObserve MonitorMode = "observe"
	ModeNote    MonitorMode = "note"
	ModeBlock   MonitorMode = "block"
)

// Valid reports whether m is one of the three modes.
func (m MonitorMode) Valid() bool { return m == ModeObserve || m == ModeNote || m == ModeBlock }

// Confidence says how sure an estimated saving is.
type Confidence string

// The three confidence levels.
const (
	ConfidenceCertain Confidence = "certain"
	ConfidenceLikely  Confidence = "likely"
	ConfidenceNone    Confidence = "none"
)

// Meta keys: the verdict pointer (history puts *CallVerdicts there) and the read-only ring view.
const (
	MetaMonitors = "monitors"
	MetaRing     = "history.ring"
)

// MaxNotesPerCall caps the notes a single call may carry (ruling R8).
const MaxNotesPerCall = 2

// MonitorEvidence is why a monitor fired. Hashes, counts and fixed phrases only, never message text (R7).
type MonitorEvidence struct {
	MatchedCalls []int    `json:"matchedCalls,omitempty"`
	Tool         string   `json:"tool,omitempty"`
	ArgsHash     string   `json:"argsHash,omitempty"`
	PathHash     string   `json:"pathHash,omitempty"`
	Path         string   `json:"path,omitempty"`
	Hash         string   `json:"hash,omitempty"`
	Phrases      []string `json:"phrases,omitempty"`
	Count        int      `json:"count,omitempty"`
	Threshold    int      `json:"threshold,omitempty"`
}

// EstBasis holds the numbers the estimator needs to price a verdict.
type EstBasis struct {
	PriorInputTokens   int64 `json:"priorInputTokens,omitempty"`
	BodyDeltaBytes     int64 `json:"bodyDeltaBytes,omitempty"`
	MedianOutputTokens int64 `json:"medianOutputTokens,omitempty"`
}

// MonitorVerdict is one monitor's decision about one call. Mode is configured, Action is what was done.
type MonitorVerdict struct {
	Monitor  MonitorName     `json:"monitor"`
	Mode     MonitorMode     `json:"mode"`
	Action   MonitorMode     `json:"action"`
	Evidence MonitorEvidence `json:"evidence"`
	Basis    EstBasis        `json:"basis"`
}

// CallVerdicts collects the verdicts of one call across the hook chain. It is safe for concurrent use.
type CallVerdicts struct {
	mu     sync.Mutex
	list   []MonitorVerdict
	notes  int
	sealed bool
}

// Add records v. It returns false, and records nothing, after Seal or when v.Monitor already has a verdict.
func (c *CallVerdicts) Add(v MonitorVerdict) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sealed {
		return false
	}
	for _, e := range c.list {
		if e.Monitor == v.Monitor {
			return false
		}
	}
	c.list = append(c.list, v)
	return true
}

// AddNotes counts n notes injected into this call, capped at MaxNotesPerCall. Ignored after Seal or n <= 0.
func (c *CallVerdicts) AddNotes(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sealed || n <= 0 {
		return
	}
	c.notes = min(c.notes+n, MaxNotesPerCall)
}

// Seal freezes the collection and returns a copy of the verdicts (in Add order) and the note count.
// It is idempotent: later calls return the same values.
func (c *CallVerdicts) Seal() ([]MonitorVerdict, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sealed = true
	return append([]MonitorVerdict(nil), c.list...), c.notes
}

// CallSummary is one call as the history ring keeps it: indexes, hashes and token counts, never text.
type CallSummary struct {
	Index       int
	Tools       []string
	ArgsHashes  []uint64
	ReadHashes  []uint64
	WroteHashes []uint64
	TextHash    uint64
	Shingles    []uint64
	Input       int64
	Output      int64
	BodyBytes   int
}

// RingView is an immutable snapshot of a session's recent calls, newest last, at most 50.
type RingView interface{ Calls() []CallSummary }

// MonitorEventRecord is one monitor_events row. Feedback is nil, "correct" or "wrong".
type MonitorEventRecord struct {
	ID            int64
	OrgID         string
	WorkspaceID   string
	SessionID     string
	CallID        string
	CallIndex     int
	TS            time.Time
	Monitor       MonitorName
	Mode          MonitorMode
	Action        MonitorMode
	Evidence      MonitorEvidence
	EstSavedToken int64
	EstSavedUSD   MicroUSD
	Confidence    Confidence
	Feedback      *string
	CreatedAt     time.Time
}

// MonitorFeedback sets or clears feedback on one event of a workspace. A foreign or unknown event is not found.
type MonitorFeedback interface {
	SetFeedback(ctx context.Context, workspaceID string, eventID int64, fb *string) (MonitorEventRecord, error)
}
