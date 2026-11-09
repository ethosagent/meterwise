package wsauth_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/wsauth"
)

func TestRedactReplacesOnlyWorkspaceURLs(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"/w/" + curID + "/openai/v1/chat", "/w/<redacted>/openai/v1/chat"},
		{"https://proxy.example.com/w/" + curID, "https://proxy.example.com/w/<redacted>"},
		{"a /w/" + curID + "/x and /w/" + oldID + "/y", "a /w/<redacted>/x and /w/<redacted>/y"},
		{"/w/short/x", "/w/short/x"},
		{"/openai/v1/chat", "/openai/v1/chat"},
		{"", ""},
	} {
		if got := wsauth.Redact(tc.in); got != tc.want {
			t.Errorf("Redact(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRedactingHandlerKeepsThePublicIDOutOfEveryLogField(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(wsauth.RedactingHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	log = log.With("route", "/w/"+curID+"/openai").WithGroup("req")
	log.Info("served /w/"+curID+"/openai/v1/chat",
		"path", "/w/"+curID+"/anthropic/v1/messages",
		"public_id", curID,
		"err", errors.New("dial https://up.example/w/"+oldID+"/x: refused"),
		slog.Group("nested", slog.String("url", "http://h/w/"+curID)),
		"status", 404)
	out := buf.String()
	for _, id := range []string{curID, oldID} {
		if strings.Contains(out, id) {
			t.Fatalf("log line carries a public id %q:\n%s", id, out)
		}
	}
	for _, want := range []string{"/w/<redacted>/anthropic/v1/messages", `"status":404`, "refused"} {
		if !strings.Contains(out, want) {
			t.Errorf("redaction removed more than the id: missing %q in\n%s", want, out)
		}
	}
	if !log.Handler().Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("the wrapper must pass the level through")
	}
}
