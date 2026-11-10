package contract

import "time"

// RFC3339 is the one wire form of a timestamp: UTC, second precision. The API, the store and the
// CLI all format through it.
func RFC3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// UTCDayStart is midnight UTC of the UTC day that contains t: the start of every "today" window
// (the stats range, the setup page's calls today).
func UTCDayStart(t time.Time) time.Time { return t.UTC().Truncate(24 * time.Hour) }
