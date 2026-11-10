package contract

import (
	"strconv"
	"strings"
)

// Page-size bounds shared by every paged list (audit log, sessions). An explicit limit outside
// MinPageLimit..MaxPageLimit is a 422; omit the parameter to get the default.
const (
	MinPageLimit     = 1
	MaxPageLimit     = 200
	DefaultPageLimit = 50
)

// LimitMessage is the one validation message for a bad page limit.
var LimitMessage = "must be a whole number from " + strconv.Itoa(MinPageLimit) + " to " + strconv.Itoa(MaxPageLimit)

// ParsePageLimit reads the optional ?limit= value. An empty raw string returns 0, which the store reads
// as "use the default". Anything else must be a whole number within the bounds.
func ParsePageLimit(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < MinPageLimit || n > MaxPageLimit {
		return 0, Invalid("limit", LimitMessage)
	}
	return n, nil
}

// ResolvePageLimit turns the parsed limit into the page size: 0 (not asked) is DefaultPageLimit, anything
// outside MinPageLimit..MaxPageLimit is a 422 on "limit". The store applies it, so every list agrees.
func ResolvePageLimit(n int) (int, error) {
	switch {
	case n == 0:
		return DefaultPageLimit, nil
	case n < MinPageLimit || n > MaxPageLimit:
		return 0, Invalid("limit", LimitMessage)
	}
	return n, nil
}
