package contract

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxNameRunes is the longest workspace or group name.
const MaxNameRunes = 80

// Invalid is the 422 validation_failed error for one field: details lists "field: msg".
func Invalid(field, msg string) *Error {
	return NewError(CodeValidationFailed, "request validation failed", field+": "+msg)
}

// ValidName trims a workspace or group name and checks it: 1 to MaxNameRunes runes and no control
// characters. It returns the trimmed name or a validation error on the field "name".
func ValidName(raw string) (string, error) {
	n := strings.TrimSpace(raw)
	switch {
	case n == "" || utf8.RuneCountInString(n) > MaxNameRunes:
		return "", Invalid("name", "must be 1-80 characters")
	case strings.IndexFunc(n, unicode.IsControl) >= 0:
		return "", Invalid("name", "must not contain control characters")
	}
	return n, nil
}
