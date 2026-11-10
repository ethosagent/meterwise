package wsauth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Redacted is what a workspace public id is replaced with in logs and traces.
const Redacted = "/w/<redacted>"

// Redact replaces every /w/{publicId} in s with /w/<redacted>. The public id is the credential of
// the workspace URL (decision D6), so it never reaches a log line, a span attribute or an error
// message that is logged.
func Redact(s string) string {
	if !strings.Contains(s, prefix) {
		return s
	}
	var b strings.Builder
	for {
		i := strings.Index(s, prefix)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		tail := s[i+len(prefix):]
		if len(tail) >= contract.PublicIDLength && contract.ValidPublicID(tail[:contract.PublicIDLength]) {
			b.WriteString(Redacted)
			s = tail[contract.PublicIDLength:]
			continue
		}
		b.WriteString(prefix)
		s = tail
	}
}

// MaskIDs replaces every token that has the shape of a public id (a maximal run of exactly
// PublicIDLength ASCII letters and digits) with "<redacted>", wherever it sits and whatever the
// text around it. It is the net under Redact for ids logged outside a /w/ URL. Like
// contract.ValidPublicID it judges shape only, so a 22-character alphanumeric word is masked too.
func MaskIDs(s string) string {
	var b strings.Builder
	last := 0
	for i := 0; i < len(s); {
		if !isAlnum(s[i]) {
			i++
			continue
		}
		j := i
		for j < len(s) && isAlnum(s[j]) {
			j++
		}
		if j-i == contract.PublicIDLength && contract.ValidPublicID(s[i:j]) {
			b.WriteString(s[last:i])
			b.WriteString("<redacted>")
			last = j
		}
		i = j
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

func isAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

// RedactingHandler wraps h so that every message and every attribute value (and the attributes
// bound with With) passes through Redact and MaskIDs: strings, errors, Stringers, *url.URL and any
// other value, whatever the attribute is called. An attribute named public_id or publicId is masked
// whole. Non-string values without an id in their text keep their type. It costs a substring search
// and one scan per string; a line without /w/ or an id-shaped token is not copied.
func RedactingHandler(h slog.Handler) slog.Handler { return redactor{h} }

type redactor struct{ next slog.Handler }

func (r redactor) Enabled(ctx context.Context, l slog.Level) bool { return r.next.Enabled(ctx, l) }

func (r redactor) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, MaskIDs(Redact(rec.Message)), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return r.next.Handle(ctx, out)
}

func (r redactor) WithAttrs(attrs []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		red[i] = redactAttr(a)
	}
	return redactor{r.next.WithAttrs(red)}
}

func (r redactor) WithGroup(name string) slog.Handler { return redactor{r.next.WithGroup(name)} }

func redactAttr(a slog.Attr) slog.Attr {
	if a.Key == "public_id" || a.Key == "publicId" {
		return slog.String(a.Key, "<redacted>")
	}
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, MaskIDs(Redact(v.String())))
	case slog.KindGroup:
		g := v.Group()
		red := make([]any, len(g))
		for i, ga := range g {
			red[i] = redactAttr(ga)
		}
		return slog.Group(a.Key, red...)
	case slog.KindAny:
		// errors, Stringers, *url.URL, structs, maps: judge them by their printed form (fmt calls
		// Error and String and recovers from a panicking method).
		txt := fmt.Sprint(v.Any())
		if red := MaskIDs(Redact(txt)); red != txt {
			return slog.String(a.Key, red)
		}
	}
	return slog.Attr{Key: a.Key, Value: v}
}
