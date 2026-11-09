package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/identity"
	"github.com/ethosagent/meterwise/internal/nonet"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

// syncBuf is a bytes.Buffer the test can read while run writes to it.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.b.Bytes()...)
}
func (s *syncBuf) String() string { return string(s.Bytes()) }

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().String()
}

// The minted tokens verify with the production Clerk adapter over the HTTPS listener (trusting
// only the written certificate), and an expired email is rejected as unauthenticated.
func TestMintedTokensVerifyThroughTheAdapterOverTLS(t *testing.T) {
	tlsAddr, httpAddr := freeAddr(t), freeAddr(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	var out, errb syncBuf
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"-listen-tls", tlsAddr, "-listen-http", httpAddr, "-public-host", "localhost", "-ca-out", ca}, &out, &errb)
	}()
	t.Cleanup(func() { cancel(); <-done })

	var env map[string]string
	for range 100 {
		if err := json.Unmarshal(out.Bytes(), &env); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if env == nil {
		t.Fatalf("no startup line; stderr: %s", errb.String())
	}
	pemBytes, err := os.ReadFile(ca)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		t.Fatal("the certificate is not PEM")
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	idp, err := identity.NewClerk(identity.ClerkConfig{Issuer: env["CLERK_ISSUER"], JWKSURL: env["CLERK_JWKS_URL"], HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	mintAs := func(email, extra string) string {
		req, err := http.NewRequest(http.MethodGet, env["MINT_URL"]+"?name=Ada&email="+email+extra, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "http://localhost:8081")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var b struct{ Token string }
		if err := json.NewDecoder(resp.Body).Decode(&b); err != nil || b.Token == "" {
			t.Fatalf("mint: %v %+v", err, b)
		}
		if resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:8081" {
			t.Fatalf("the mint does not echo a loopback origin: %q", resp.Header.Get("Access-Control-Allow-Origin"))
		}
		return b.Token
	}
	mint := func(email string) string { return mintAs(email, "") }
	id, err := idp.Verify(context.Background(), mint("Ada@Example.test"))
	if err != nil || id.Email != "ada@example.test" || !id.EmailVerified || !strings.HasPrefix(id.Subject, "user_") {
		t.Fatalf("verify: %+v %v", id, err)
	}
	if id2, _ := idp.Verify(context.Background(), mint("ada@example.test")); id2.Subject != id.Subject {
		t.Fatal("the subject is not stable per email")
	}
	// A person who deleted their account and registered again has the same email and a new subject.
	if again, err := idp.Verify(context.Background(), mintAs("ada@example.test", "&sub=user_registered_again")); err != nil || again.Subject != "user_registered_again" || again.Email != id.Email {
		t.Fatalf("explicit subject: %+v %v", again, err)
	}
	// unverified=1 mints a token whose email claim Clerk has not verified (the attacker's token).
	if spoof, err := idp.Verify(context.Background(), mintAs("ada@example.test", "&unverified=1")); err != nil || spoof.EmailVerified || spoof.Email != "ada@example.test" {
		t.Fatalf("unverified mint: %+v %v", spoof, err)
	}
	if resp, err := http.Get(env["MINT_URL"] + "?email=ada@example.test&sub=evil%20sub"); err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a malformed subject must be refused: %v %v", resp, err)
	}
	post := func(path string) {
		resp, err := http.Post("http://"+httpAddr+path, "", nil)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: %v %v", path, resp, err)
		}
		_ = resp.Body.Close()
	}
	for _, evil := range []string{"https://evil.example", "http://localhost.evil.example", "http://127.0.0.1.evil.example:80", "null", "http://u@localhost:1"} {
		for _, call := range [][2]string{{"GET", env["MINT_URL"] + "?email=ada@example.test"}, {"POST", "http://" + httpAddr + "/admin/expire?email=ada@example.test"}} {
			req, _ := http.NewRequest(call[0], call[1], nil)
			req.Header.Set("Origin", evil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden || resp.Header.Get("Access-Control-Allow-Origin") != "" {
				t.Fatalf("%s %s from origin %q: %d", call[0], call[1], evil, resp.StatusCode)
			}
		}
	}
	post("/admin/expire?email=ada@example.test")
	if _, err := idp.Verify(context.Background(), mint("ada@example.test")); contract.AsError(err).Code != contract.CodeUnauthenticated {
		t.Fatalf("expired mint: %v", err)
	}
	post("/admin/restore?email=ada@example.test")
	if _, err := idp.Verify(context.Background(), mint("ada@example.test")); err != nil {
		t.Fatalf("restored mint: %v", err)
	}
}

func TestCAOutIsRequired(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), nil, &out, &errb); code != 2 || !strings.Contains(errb.String(), "-ca-out") {
		t.Fatalf("code %d, %s", code, errb.String())
	}
}
