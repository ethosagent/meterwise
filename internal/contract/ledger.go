package contract

// LedgerTables are the append-only ledger tables. Only the features that own them
// (internal/ledger and internal/audit) may write them: archtest rule H4 and the
// migration lint in cmd/gen both build from this one list.
var LedgerTables = []string{"calls", "sessions", "monitor_events", "audit_log"}
