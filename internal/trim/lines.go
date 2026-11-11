package trim

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// itoa is strconv.Itoa; the markers are built by hand so they never go through fmt.
func itoa(n int) string { return strconv.Itoa(n) }

// jsonLen is the length of s as a JSON string with its quotes, as internal/wire encodes it (no HTML escaping). It
// lets the hook size a rewrite without encoding it.
func jsonLen(s string) int {
	n := 2
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch {
			case c == '"' || c == '\\' || c == '\n' || c == '\r' || c == '\t' || c == '\b' || c == '\f':
				n += 2
			case c < 0x20:
				n += 6
			default:
				n++
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			n += 3 // an invalid byte is written as the replacement character (three bytes), not as `\uXXXX` escapes
		case r == ' ' || r == ' ':
			n += 6
		default:
			n += size
		}
		i += size
	}
	return n
}

// readTool reports whether a tool name looks like one that returns file content: windowing source code would lose
// what the model asked to read, so log_window leaves such results alone.
func readTool(name string) bool {
	l := strings.ToLower(name)
	if l == "cat" {
		return true
	}
	for _, w := range [...]string{"read", "view", "file", "open"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// isDigit reports an ASCII digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// sameTemplate reports whether two lines are equal once every run of digits is treated as one placeholder, so
// lines that differ only in a counter, a line number or a timestamp match.
func sameTemplate(a, b string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case isDigit(a[i]) && isDigit(b[j]):
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
		case a[i] == b[j]:
			i++
			j++
		default:
			return false
		}
	}
	return i == len(a) && j == len(b)
}

// substance counts the bytes of s that are not whitespace.
func substance(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if c := s[i]; c != ' ' && c != '\t' && c != '\r' {
			n++
		}
	}
	return n
}
