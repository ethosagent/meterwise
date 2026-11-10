package wsauth_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/url"
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

type stringer struct{ id string }

func (s stringer) String() string { return "ws=" + s.id }

type plain struct {
	Name string
	Ref  string
}

func TestRedactingHandlerMasksAnyIDShapedTokenWhateverTheAttrNameOrKind(t *testing.T) {
	u, _ := url.Parse("https://h.example/hook?ws=" + curID)
	for name, attr := range map[string]slog.Attr{
		"string":   slog.String("workspace", curID),
		"embedded": slog.String("note", "got "+curID+", then "+oldID+"."),
		"error":    slog.Any("cause", errors.New("lookup "+curID+" failed")),
		"stringer": slog.Any("who", stringer{curID}),
		"url":      slog.Any("target", u),
		"struct":   slog.Any("obj", plain{Name: "x", Ref: curID}),
		"map":      slog.Any("m", map[string]string{"k": curID}),
		"group":    slog.Group("g", slog.String("deep", curID)),
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			log := slog.New(wsauth.RedactingHandler(slog.NewJSONHandler(&buf, nil)))
			log.Info("msg "+curID, attr, "status", 404, "ratio", 0.5, "ok", true)
			out := buf.String()
			for _, id := range []string{curID, oldID} {
				if strings.Contains(out, id) {
					t.Fatalf("public id %q leaked:\n%s", id, out)
				}
			}
			for _, want := range []string{`"status":404`, `"ratio":0.5`, `"ok":true`} {
				if !strings.Contains(out, want) {
					t.Errorf("non-string attrs must keep their type: missing %q in\n%s", want, out)
				}
			}
		})
	}
}

func TestMaskIDsLeavesOtherTokensAlone(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"short", "short"},
		{curID + "x", curID + "x"}, // 23 chars: not a public id
		{"x" + curID, "x" + curID},
		{"a_" + curID + "-b", "a_<redacted>-b"},
		{"", ""},
	} {
		if got := wsauth.MaskIDs(tc.in); got != tc.want {
			t.Errorf("MaskIDs(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
