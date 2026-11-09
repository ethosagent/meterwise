package contract

import (
	"net/mail"
	"strings"
)

// MaxEmailLength is the longest address NormalizeEmail accepts (RFC 5321 path limit).
const MaxEmailLength = 254

// NormalizeEmail is the one email normaliser: it returns the lower-cased address and true, or
// "" and false when raw is not a plain address. Plain means printable ASCII only, no whitespace
// of any kind anywhere (Unicode included), at most MaxEmailLength bytes, and exactly one bare
// address as net/mail parses it (no display name, no angle brackets).
//
// It never trims. Unicode whitespace and non-ASCII are judged on the raw value, before any case
// folding: Unicode folding maps some characters to ASCII (U+212A KELVIN SIGN to "k", U+0130 to
// "i"), which would let a different address claim an invitation written for another. Callers
// that take human input trim ASCII whitespace themselves, knowing that they do.
func NormalizeEmail(raw string) (string, bool) {
	if raw == "" || len(raw) > MaxEmailLength {
		return "", false
	}
	for _, r := range raw {
		if r <= ' ' || r > '~' {
			return "", false
		}
	}
	e := strings.ToLower(raw)
	addr, err := mail.ParseAddress(e)
	if err != nil || addr.Address != e || addr.Name != "" {
		return "", false
	}
	return e, true
}

// NotInvited is the 403 for an identity that holds no active membership and no pending invitation.
func NotInvited() *Error {
	return NewError(CodeNotInvited, "this account has not been invited to the organization")
}
