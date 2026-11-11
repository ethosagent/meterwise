package nonet_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/nonet"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

func TestAllowed(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1:5432": true, "[::1]:80": true, "localhost:8080": true,
		"192.0.2.1:80": false, "example.com:443": false, "10.0.0.5:5432": false,
	}
	for addr, want := range tests {
		if got := nonet.Allowed(addr); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestExternalDialAndDNSAreBlocked(t *testing.T) {
	if _, err := nonet.Dial(context.Background(), "tcp", "192.0.2.1:80"); !errors.Is(err, nonet.ErrBlocked) {
		t.Fatalf("dial: want ErrBlocked, got %v", err)
	}
	// net.DNSError does not wrap its cause, so match the message.
	if _, err := net.DefaultResolver.LookupHost(context.Background(), "example.com"); err == nil || !strings.Contains(err.Error(), nonet.ErrBlocked.Error()) {
		t.Fatalf("lookup: want ErrBlocked, got %v", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://192.0.2.1/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := http.DefaultClient.Do(req); !errors.Is(err, nonet.ErrBlocked) {
		if resp != nil {
			_ = resp.Body.Close()
		}
		t.Fatalf("http: want ErrBlocked, got %v", err)
	}
}

func TestLoopbackStaysOpen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer srv.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("loopback request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestSkipTimingSkipsExactlyUnderShort(t *testing.T) {
	var sub *testing.T
	t.Run("probe", func(st *testing.T) {
		sub = st
		nonet.SkipTiming(st)
	})
	if got := sub.Skipped(); got != testing.Short() {
		t.Fatalf("skipped = %v under -short = %v", got, testing.Short())
	}
}

func TestRestore(t *testing.T) {
	restore := nonet.Install()
	restore()
	if _, ok := http.DefaultTransport.(*http.Transport); !ok {
		t.Fatal("default transport changed type")
	}
}
