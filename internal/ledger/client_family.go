package ledger

import (
	"net/http"
	"strings"
)

// maxClientFamily caps a client family in bytes (the calls.client and sessions.client columns are text, but the
// dashboard groups by it, so it stays short).
const maxClientFamily = 64

// ClientFamily names the client of a request from its User-Agent alone: the first product token, without its
// version, lower-cased, printable ASCII only and at most 64 bytes ("opencode/1.2 (linux)" is "opencode").
// "" means the header is absent or has no usable token; the store keeps that as NULL and the UI shows
// "unknown". No credential header is read (rule 2): only User-Agent is looked at.
//
// Only strings that appear in committed fixtures are special-cased, and today none needs a special case, so
// every family is the generic product token: opencode (internal/ledger/helpers_test.go) and demo-traffic
// (cmd/demo-traffic). SDK and CLI User-Agents that no fixture holds (Claude Code, Codex, the OpenAI and
// Anthropic SDKs, curl) are not guessed: they reach the dashboard as whatever product token they send.
func ClientFamily(h http.Header) string {
	tok, _, _ := strings.Cut(strings.TrimSpace(h.Get("User-Agent")), " ")
	tok, _, _ = strings.Cut(tok, "/")
	var b strings.Builder
	for i := 0; i < len(tok) && b.Len() < maxClientFamily; i++ {
		if c := tok[i]; c > ' ' && c < 0x7f {
			b.WriteByte(lowerASCII(c))
		}
	}
	return b.String()
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}
