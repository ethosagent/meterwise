package contract

import "unicode/utf8"

// TruncateUTF8 returns s cut to at most n bytes without splitting a multi-byte character: the longest
// prefix of s that is at most n bytes and ends on a character boundary. A valid s stays valid; s is
// returned as it is when it already fits. history, monitors and the ledger share it.
func TruncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	n = max(n, 0)
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
