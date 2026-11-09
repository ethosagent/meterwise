package contract

// Limits shared by the API, the store and the web client.
const (
	// DefaultGraceMinutes is how long the previous proxy URL keeps working after a reset when the
	// request does not say.
	DefaultGraceMinutes = 10
	// MaxGraceMinutes is the longest grace window a reset may ask for (24 hours).
	MaxGraceMinutes = 24 * 60
)
