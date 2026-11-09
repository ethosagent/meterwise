package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ethosagent/meterwise/internal/identity/clerkmock"
)

const readHeaderTimeout = 5 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// minter signs tokens for the web dev adapter and remembers which emails are "expired".
type minter struct {
	mock    *clerkmock.Server
	issuer  string
	mu      sync.Mutex
	expired map[string]bool
}

var validSubject = regexp.MustCompile(`^user_[A-Za-z0-9_]{1,40}$`)

func subjectFor(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(email)))
	return "user_" + hex.EncodeToString(sum[:6])
}

func (m *minter) token(email, name, sub string, unverified bool) string {
	email = strings.ToLower(strings.TrimSpace(email))
	m.mu.Lock()
	gone := m.expired[email]
	m.mu.Unlock()
	if sub == "" {
		sub = subjectFor(email)
	}
	c := clerkmock.Claims{Subject: sub, Email: email, Name: name, Issuer: m.issuer, Unverified: unverified}
	if gone {
		c.ExpiresIn = -time.Hour
	}
	return m.mock.Token(c)
}

func (m *minter) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mint", func(w http.ResponseWriter, r *http.Request) {
		email := r.URL.Query().Get("email")
		if !strings.Contains(email, "@") {
			http.Error(w, `{"error":"email is required"}`, http.StatusBadRequest)
			return
		}
		// unverified=1 mints an email claim Clerk has not verified, to show the server ignores it.
		// sub is optional: a person who deleted their account and registered again keeps the email
		// and gets a new provider user id.
		sub := r.URL.Query().Get("sub")
		if sub != "" && !validSubject.MatchString(sub) {
			http.Error(w, `{"error":"sub must look like user_<letters or digits>"}`, http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"token": m.token(email, r.URL.Query().Get("name"), sub, r.URL.Query().Get("unverified") == "1")})
	})
	setExpired := func(v bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
			m.mu.Lock()
			m.expired[email] = v
			m.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"email": email, "expired": v})
		}
	}
	mux.HandleFunc("POST /admin/expire", setExpired(true))
	mux.HandleFunc("POST /admin/restore", setExpired(false))
	mux.HandleFunc("GET /admin/state", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"invitations": m.mock.Invitations()})
	})
	return cors(mux)
}

// cors lets a page on a loopback origin call the mint and refuses every other
// browser origin (a request with no Origin header is a script, and passes). The
// mint hands out signed tokens for any email, so a page on the open web must not
// be able to read it or hit the admin endpoints (a simple POST carries an Origin).
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			if !loopbackOrigin(origin) {
				http.Error(w, `{"error":"origin not allowed"}`, http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loopbackOrigin reports whether origin is http(s)://localhost, 127.0.0.1 or [::1], any port.
func loopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" {
		return false
	}
	h := u.Hostname()
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// selfSigned makes a certificate for hosts that is its own trust anchor.
func selfSigned(hosts []string) (tls.Certificate, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "clerkmock-serve"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(48 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true,
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else {
			tpl.DNSNames = append(tpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pemBytes, nil
}

type options struct {
	tlsAddr, httpAddr, publicHost, caOut string
}

func parseOptions(args []string, stderr io.Writer) (options, int) {
	fs := flag.NewFlagSet("clerkmock-serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o options
	fs.StringVar(&o.tlsAddr, "listen-tls", "127.0.0.1:9443", "HTTPS listener: JWKS and the backend invitations API")
	fs.StringVar(&o.httpAddr, "listen-http", "127.0.0.1:9444", "plain HTTP listener: token mint and admin")
	fs.StringVar(&o.publicHost, "public-host", "host.docker.internal", "host name the process under test uses to reach the HTTPS listener")
	fs.StringVar(&o.caOut, "ca-out", "", "write the self-signed certificate here (trust it with SSL_CERT_FILE); required")
	if err := fs.Parse(args); err != nil {
		return o, 2
	}
	if o.caOut == "" {
		fmt.Fprintln(stderr, "clerkmock-serve: -ca-out is required")
		return o, 2
	}
	return o, 0
}

// listeners binds both addresses up front so a busy port fails before anything is announced.
func listeners(ctx context.Context, o options) (tlsLn, httpLn net.Listener, err error) {
	var lc net.ListenConfig
	if tlsLn, err = lc.Listen(ctx, "tcp", o.tlsAddr); err != nil {
		return nil, nil, err
	}
	if httpLn, err = lc.Listen(ctx, "tcp", o.httpAddr); err != nil {
		_ = tlsLn.Close()
		return nil, nil, err
	}
	return tlsLn, httpLn, nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	o, code := parseOptions(args, stderr)
	if code != 0 {
		return code
	}
	if err := serve(ctx, o, stdout); err != nil {
		fmt.Fprintln(stderr, "clerkmock-serve:", err)
		return 1
	}
	return 0
}

// announce prints what the process under test needs, as one JSON object.
func announce(w io.Writer, o options, issuer, webhookSecret string) {
	_ = json.NewEncoder(w).Encode(map[string]string{
		"CLERK_ISSUER": issuer, "CLERK_JWKS_URL": issuer + "/.well-known/jwks.json", "CLERK_API_URL": issuer + "/v1",
		"CLERK_SECRET_KEY": clerkmock.BackendKey(), "CLERK_WEBHOOK_SECRET": webhookSecret,
		"MINT_URL": "http://" + o.httpAddr + "/mint", "CA_FILE": o.caOut,
	})
}

func serve(ctx context.Context, o options, stdout io.Writer) error {
	mock, err := clerkmock.New()
	if err != nil {
		return err
	}
	defer mock.Close()
	_, port, err := net.SplitHostPort(o.tlsAddr)
	if err != nil {
		return fmt.Errorf("-listen-tls: %w", err)
	}
	cert, pemBytes, err := selfSigned([]string{o.publicHost, "localhost", "127.0.0.1"})
	if err != nil {
		return fmt.Errorf("certificate: %w", err)
	}
	if err := os.WriteFile(o.caOut, pemBytes, 0o600); err != nil {
		return err
	}
	target, err := url.Parse(mock.Issuer())
	if err != nil {
		return err
	}
	issuer := "https://" + net.JoinHostPort(o.publicHost, port)
	m := &minter{mock: mock, issuer: issuer, expired: map[string]bool{}}
	tlsSrv := &http.Server{
		Handler: httputil.NewSingleHostReverseProxy(target), ReadHeaderTimeout: readHeaderTimeout,
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
	}
	httpSrv := &http.Server{Handler: m.handler(), ReadHeaderTimeout: readHeaderTimeout}
	tlsLn, httpLn, err := listeners(ctx, o)
	if err != nil {
		return err
	}
	errs := make(chan error, 2)
	go func() { errs <- tlsSrv.ServeTLS(tlsLn, "", "") }()
	go func() { errs <- httpSrv.Serve(httpLn) }()
	announce(stdout, o, issuer, mock.WebhookSecret())
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-errs:
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
	}
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), readHeaderTimeout)
	defer cancel()
	_ = tlsSrv.Shutdown(shutdown)
	_ = httpSrv.Shutdown(shutdown)
	return serveErr
}
