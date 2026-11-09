package wsauth

import (
	"context"
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

// RedactingHandler wraps h so that every message and every string, error or group attribute (and
// the attributes bound with With) passes through Redact, and an attribute named public_id or
// publicId is masked whole. It costs a substring search per string; a line without /w/ is not
// copied.
func RedactingHandler(h slog.Handler) slog.Handler { return redactor{h} }

type redactor struct{ next slog.Handler }

func (r redactor) Enabled(ctx context.Context, l slog.Level) bool { return r.next.Enabled(ctx, l) }

func (r redactor) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, Redact(rec.Message), rec.PC)
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
		return slog.String(a.Key, Redact(v.String()))
	case slog.KindGroup:
		g := v.Group()
		red := make([]any, len(g))
		for i, ga := range g {
			red[i] = redactAttr(ga)
		}
		return slog.Group(a.Key, red...)
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			return slog.String(a.Key, Redact(err.Error()))
		}
	}
	return slog.Attr{Key: a.Key, Value: v}
}
