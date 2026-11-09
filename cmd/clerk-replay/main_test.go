package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/identity"
	"github.com/ethosagent/meterwise/internal/identity/clerkmock"
	"github.com/ethosagent/meterwise/internal/nonet"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

const testSecret = "whsec_ZmFrZS1zZWNyZXQta2V5LWZvci10ZXN0cw=="

// The receiver verifies with the production verifier, so the replay is proved to
// pass signature verification, not to bypass it.
func TestReplayDeliversEveryFixtureWithAValidSignature(t *testing.T) {
	v, err := identity.NewWebhookVerifier(testSecret, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ev, err := v.Verify(body, r.Header)
		if err != nil {
			http.Error(w, "rejected", http.StatusBadRequest)
			return
		}
		v.MarkProcessed(ev.ID)
		got = append(got, ev.Type)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	var out, errb bytes.Buffer
	code := run([]string{"-url", srv.URL, "-fixture", "all"}, func(k string) string {
		if k == envWebhookSigning {
			return testSecret
		}
		return ""
	}, &out, &errb)
	if code != 0 || len(got) != len(clerkmock.FixtureNames()) || !strings.Contains(out.String(), "user.created") {
		t.Fatalf("code %d, delivered %v, out %q err %q", code, got, out.String(), errb.String())
	}
}

func TestReplayWithTheWrongSecretIsRejected(t *testing.T) {
	v, _ := identity.NewWebhookVerifier(testSecret, time.Now)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if _, err := v.Verify(body, r.Header); err != nil {
			http.Error(w, "rejected", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	var out, errb bytes.Buffer
	code := run([]string{"-url", srv.URL, "-fixture", "user.created", "-secret", "whsec_d3Jvbmctc2VjcmV0LXdyb25nLXNlY3JldA=="}, func(string) string { return "" }, &out, &errb)
	if code != 1 || !strings.Contains(out.String(), "FAILED") {
		t.Fatalf("code %d out %q", code, out.String())
	}
}

func TestReplayUsageErrors(t *testing.T) {
	none := func(string) string { return "" }
	var out, errb bytes.Buffer
	if code := run(nil, none, &out, &errb); code != 2 || !strings.Contains(errb.String(), envWebhookSigning) {
		t.Fatalf("no secret: %d %q", code, errb.String())
	}
	if code := run([]string{"-secret", testSecret, "-fixture", "nope"}, none, &out, &errb); code != 2 {
		t.Fatalf("unknown fixture: %d", code)
	}
	if code := run([]string{"-bogus"}, none, &out, &errb); code != 2 {
		t.Fatalf("bad flag: %d", code)
	}
}
