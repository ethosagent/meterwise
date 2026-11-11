package nonet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
)

// ErrBlocked is returned for any dial or lookup the guard refuses.
var ErrBlocked = errors.New("nonet: external network is blocked in tests")

// Allowed reports whether addr ("host:port") is loopback, a unix socket path
// or the literal name localhost.
func Allowed(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Dial is a net.Dialer.DialContext that only reaches loopback.
func Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	switch network {
	case "unix", "unixgram", "unixpacket":
	default:
		if !Allowed(addr) {
			return nil, fmt.Errorf("dial %s %s: %w", network, addr, ErrBlocked)
		}
	}
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

// Install swaps the process-wide resolver and default HTTP transport for
// guarded ones and returns a function that restores them.
func Install() (restore func()) {
	oldResolver := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, fmt.Errorf("dns: %w", ErrBlocked)
		},
	}
	var oldDial func(context.Context, string, string) (net.Conn, error)
	tr, ok := http.DefaultTransport.(*http.Transport)
	if ok {
		oldDial = tr.DialContext
		tr.DialContext = Dial
	}
	return func() {
		net.DefaultResolver = oldResolver
		if ok {
			tr.DialContext = oldDial
		}
	}
}

// SkipTiming skips a test that asserts a wall-clock budget (a p99, a p95, a
// "took less than N ms" limit) when the tests run with -short. The parallel,
// race-instrumented `test` gate runs with -short, where machine load makes such
// limits fail on noise; the serial `bench` gate (scripts/check-bench.sh) runs
// without -short and so runs them. Call it first in every wall-time test.
// Allocation counts and relative checks are deterministic and do not use it.
func SkipTiming(t testing.TB) {
	t.Helper()
	if testing.Short() {
		t.Skip("wall-clock timing budget: runs in the serial bench gate (scripts/check-bench.sh), not under -short")
	}
}

// Run installs the guard, runs the package's tests and restores the guard.
// Use: func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }
func Run(m *testing.M) int {
	restore := Install()
	defer restore()
	return m.Run()
}
