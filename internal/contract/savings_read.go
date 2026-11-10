package contract

// RepeatTarget is one tool (and file, when one was stored) that a repetition monitor fired on, with the
// number of fires. The store reads them from monitor_events.evidence; savings.Explain turns them into
// Suggestions. It carries no message text (rule R7): Path is set only when it was stored, otherwise
// PathHash holds the hash prefix.
type RepeatTarget struct {
	Monitor  MonitorName
	Tool     string
	Path     string
	PathHash string
	Count    int64
}

// IsRepetition reports whether m fires on repeated work: the monitors whose evidence names a target.
func (m MonitorName) IsRepetition() bool {
	return m == MonitorDuplicateToolCall || m == MonitorRepeatedRead
}
