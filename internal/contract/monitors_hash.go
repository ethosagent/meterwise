package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The ring alphabet. history writes CallSummary hashes and monitors compares them, and the two pure
// packages may not import each other (ruling R1), so the one definition of "how a turn becomes
// hashes" lives here, beside CallSummary.

// ShingleSize is the token window of a ring shingle; MaxRingShingles caps how many a turn keeps.
const (
	ShingleSize     = 8
	MaxRingShingles = 64
	// ShingleTextCap bounds the text a turn's shingles are cut from (reasoning and text apart).
	ShingleTextCap = 1024
)

const (
	fnvOffset = 14695981039346656037
	fnvPrime  = 1099511628211
)

// HashString is FNV-1a 64 over s.
func HashString(s string) uint64 {
	h := uint64(fnvOffset)
	for i := 0; i < len(s); i++ {
		h = (h ^ uint64(s[i])) * fnvPrime
	}
	return h
}

// HashOf is the privacy-safe public id of a ring hash inside one workspace: 16 hex characters of
// sha256(workspaceID + ":" + the hash in decimal) (ruling R7).
func HashOf(workspaceID string, h uint64) string {
	var buf [96]byte
	b := append(buf[:0], workspaceID...)
	b = append(b, ':')
	b = strconv.AppendUint(b, h, 10)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// TextHasher hashes text as its lowercase letter/digit tokens joined by one space, so the result is
// the same however the text is cut into stream deltas. Sum is 0 for text with no token.
type TextHasher struct {
	h      uint64
	any    bool
	inWord bool
}

func (t *TextHasher) mix(b byte) {
	if !t.any {
		t.h = fnvOffset
	}
	t.h = (t.h ^ uint64(b)) * fnvPrime
}

// Write adds s to the hash.
func (t *TextHasher) Write(s string) {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			t.inWord = false
			continue
		}
		if !t.inWord && t.any {
			t.mix(' ')
		}
		t.inWord = true
		var buf [utf8.UTFMax]byte
		n := utf8.EncodeRune(buf[:], unicode.ToLower(r))
		for _, b := range buf[:n] {
			t.mix(b)
		}
		t.any = true
	}
}

// Sum returns the hash, 0 when no token was written.
func (t *TextHasher) Sum() uint64 {
	if !t.any {
		return 0
	}
	return t.h
}

// NormalizedHash hashes one whole text with a TextHasher.
func NormalizedHash(s string) uint64 {
	var t TextHasher
	t.Write(s)
	return t.Sum()
}

// Tokens splits s into lowercase letter/digit tokens, at most limit of them (limit <= 0 means all).
func Tokens(s string, limit int) []string {
	var out []string
	start := -1
	flush := func(end int) {
		if start >= 0 {
			out = append(out, strings.ToLower(s[start:end]))
			start = -1
		}
	}
	for i, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
		if limit > 0 && len(out) >= limit {
			return out
		}
	}
	flush(len(s))
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ShinglesOf hashes every window of ShingleSize tokens, in order, at most limit of them.
func ShinglesOf(tokens []string, limit int) []uint64 {
	if len(tokens) < ShingleSize || limit <= 0 {
		return nil
	}
	n := min(len(tokens)-ShingleSize+1, limit)
	out := make([]uint64, n)
	for i := range out {
		out[i] = HashString(strings.Join(tokens[i:i+ShingleSize], " "))
	}
	return out
}

// TurnShingles is the ring's shingle list of one assistant turn: the first MaxRingShingles windows of
// reasoning then text, each cut to ShingleTextCap bytes first.
func TurnShingles(reasoning, text string) []uint64 {
	toks := Tokens(capText(reasoning), 0)
	toks = append(toks, Tokens(capText(text), 0)...)
	return ShinglesOf(toks, MaxRingShingles)
}

// SortedSet sorts hashes ascending and drops repeats, in place. The ring stores a turn's shingles so, and
// the monitors compare them as sets by a merge walk (a repeated window changes no set operation).
func SortedSet(h []uint64) []uint64 {
	slices.Sort(h)
	return slices.Compact(h)
}

func capText(s string) string { return TruncateUTF8(s, ShingleTextCap) }

// ToolKind classifies a tool by its name.
type ToolKind int

// Tool kinds. A tool that may change the world (shell, edit, create...) is ToolWrite.
const (
	ToolOther ToolKind = iota
	ToolRead
	ToolWrite
)

var writeWords = wordSet("write edit create patch replace delete remove append save insert rename mkdir move " +
	"bash shell exec execute command terminal run apply commit push update sed touch mv rm cp git")
var readWords = wordSet("read view cat open fetch")

func wordSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// ClassifyTool says whether a tool name reads, writes or neither. Writes win: a name that looks like
// both (read_and_write) is a write, so a missed write never lets a monitor fire.
func ClassifyTool(name string) ToolKind {
	words := splitName(name)
	for _, w := range words {
		if writeWords[w] {
			return ToolWrite
		}
	}
	for i, w := range words {
		if readWords[w] || (w == "get" && i+1 < len(words) && words[i+1] == "file") {
			return ToolRead
		}
	}
	return ToolOther
}

// splitName splits a tool name on non-alphanumerics and camelCase boundaries, lowercased.
func splitName(name string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	var prev rune
	for _, r := range name {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r) && unicode.IsLower(prev):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
		prev = r
	}
	flush()
	return out
}
