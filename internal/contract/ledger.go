package contract

import "time"

// LedgerTables are the append-only ledger tables. Only the features that own them
// (internal/ledger and internal/audit) may write them: archtest rule H4 and the
// migration lint in cmd/gen both build from this one list.
var LedgerTables = []string{"calls", "sessions", "monitor_events", "audit_log"}

// Session sources and outcomes as stored in sessions.source and sessions.outcome.
const (
	SessionSourceHeader    = "header"
	SessionSourcePredicted = "predicted"

	OutcomeOpen      = "open"
	OutcomeCompleted = "completed"
	OutcomeAbandoned = "abandoned"
)

// CallRecord is one completed call as the ledger writes it (calls row) and the API reads it back.
// It carries token counts and names only: never a message body (privacy mode, non-negotiable 3).
type CallRecord struct {
	ID            string
	OrgID         string
	WorkspaceID   string
	SessionID     string
	CallIndex     int
	StartedAt     time.Time
	Provider      string
	Model         string
	Wire          string
	Stream        bool
	Status        int
	Finish        string
	Usage         Usage
	Cost          *MicroUSD // nil: no price for the model (flagged), never a silent 0
	TTFBMs        int64
	DurationMs    int64
	ToolCalls     []string
	Blocked       bool
	NotesInjected int
	Client        string

	// Session facts the ledger carries to the sessions upsert; not stored on the call row.
	SessionSource string // header|predicted
	SessionStart  time.Time
	LastHadText   bool   // the call's assistant text was non-empty (outcome heuristic)
	Preview       string // first user message, at most 200 chars; "" unless capture is on and privacy mode is off
}

// SessionClose asks the ledger to finalise one quiet session.
type SessionClose struct {
	WorkspaceID string
	SessionID   string
	At          time.Time
}

// SessionRecord is one sessions row.
type SessionRecord struct {
	ID            string
	WorkspaceID   string
	Source        string
	StartedAt     time.Time
	LastSeenAt    time.Time
	ClosedAt      *time.Time
	Calls         int
	InputTokens   int64
	OutputTokens  int64
	Cost          *MicroUSD
	Saved         MicroUSD
	LikelySaved   MicroUSD
	Outcome       string
	Client        string
	FirstUserPrev string
}

// SessionQuery selects one page of a workspace's sessions, newest first by (started_at, id).
// Cursor is opaque; From and To bound started_at ([From, To)); zero values are open ends.
type SessionQuery struct {
	WorkspaceID string
	Cursor      string
	From, To    time.Time
	Limit       int
}

// SessionPage is one page of sessions; NextCursor is "" on the last page.
type SessionPage struct {
	Items      []SessionRecord
	NextCursor string
}

// StatsDay is one UTC day of a workspace's ledger.
type StatsDay struct {
	Day      time.Time
	Spend    MicroUSD
	Calls    int
	Sessions int
}

// Stats is a workspace's spend and volume over a range.
type Stats struct {
	Spend    MicroUSD
	Calls    int
	Sessions int
	ByDay    []StatsDay
}

// SetupState is what the tenancy service knows about a workspace's setup: its current public id and
// the connection status. The API turns the public id into a URL through its one URL builder.
type SetupState struct {
	PublicID string
	Status   SetupStatus
}

// CallStatus is what Setup.status needs from the calls table.
type CallStatus struct {
	FirstCallAt *time.Time
	CallsToday  int
}

// PriceRow is one row of the pricing table: micro-USD per million tokens, effective from a time.
// CachedPerM and ReasoningPerM are nil when the provider has no separate rate: cached tokens then
// bill at the input rate and reasoning tokens at the output rate.
type PriceRow struct {
	Provider      string
	Model         string
	InputPerM     MicroUSD
	OutputPerM    MicroUSD
	CachedPerM    *MicroUSD
	ReasoningPerM *MicroUSD
	EffectiveFrom time.Time
}
