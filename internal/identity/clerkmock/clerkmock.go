package clerkmock

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"time"

	svix "github.com/svix/svix-webhooks/go"
)

//go:embed testdata/webhooks/*.json
var fixtures embed.FS

// BackendKey is the backend API credential the mock accepts.
func BackendKey() string { return "sk_test_clerkmock" }

// KeyID is the kid of the key the JWKS publishes.
const KeyID = "ins_mock_key_1"

const rsaBits = 2048

var (
	keyOnce sync.Once
	shared  *rsa.PrivateKey
)

// signingKey is generated once per process: the key guards nothing, and
// generating one per test server would dominate the test time.
func signingKey() *rsa.PrivateKey {
	keyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, rsaBits)
		if err != nil {
			panic("clerkmock: key: " + err.Error())
		}
		shared = k
	})
	return shared
}

// Fixture returns a committed webhook payload ("user.created", "user.updated", "user.deleted").
func Fixture(name string) ([]byte, error) {
	return fixtures.ReadFile("testdata/webhooks/" + name + ".json")
}

// FixtureNames lists the committed webhook payloads.
func FixtureNames() []string {
	ents, _ := fixtures.ReadDir("testdata/webhooks")
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		out = append(out, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(out)
	return out
}

// Invitation is one invitation the mock backend API received.
type Invitation struct {
	ID      string
	Email   string
	Revoked bool
}

// Server is the loopback Clerk twin.
type Server struct {
	srv     *httptest.Server
	key     *rsa.PrivateKey
	webhook *svix.Webhook
	secret  string

	mu         sync.Mutex
	invites    []*Invitation
	jwksHits   int
	failAPI    bool
	jwksDown   bool
	hasCreated int
}

// New starts the server; call Close when done.
func New() (*Server, error) {
	key := signingKey()
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("clerkmock: secret: %w", err)
	}
	secret := "whsec_" + base64.StdEncoding.EncodeToString(raw[:])
	wh, err := svix.NewWebhook(secret)
	if err != nil {
		return nil, fmt.Errorf("clerkmock: webhook: %w", err)
	}
	s := &Server{key: key, webhook: wh, secret: secret}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/jwks.json", s.serveJWKS)
	mux.HandleFunc("POST /v1/invitations", s.createInvitation)
	mux.HandleFunc("POST /v1/invitations/{id}/revoke", s.revokeInvitation)
	s.srv = httptest.NewServer(mux)
	return s, nil
}

// Close stops the server.
func (s *Server) Close() { s.srv.Close() }

// Issuer is the iss claim of a valid token.
func (s *Server) Issuer() string { return s.srv.URL }

// JWKSURL is where the adapter fetches signing keys.
func (s *Server) JWKSURL() string { return s.srv.URL + "/.well-known/jwks.json" }

// APIURL is the backend API base the adapter's SDK calls.
func (s *Server) APIURL() string { return s.srv.URL + "/v1" }

// WebhookSecret is the Svix signing secret (whsec_...).
func (s *Server) WebhookSecret() string { return s.secret }

// SetJWKSDown makes the JWKS endpoint answer 503.
func (s *Server) SetJWKSDown(down bool) { s.mu.Lock(); s.jwksDown = down; s.mu.Unlock() }

// SetAPIDown makes the backend API answer 503.
func (s *Server) SetAPIDown(down bool) { s.mu.Lock(); s.failAPI = down; s.mu.Unlock() }

// JWKSHits is how many times the JWKS was fetched.
func (s *Server) JWKSHits() int { s.mu.Lock(); defer s.mu.Unlock(); return s.jwksHits }

// Invitations returns a snapshot of the invitations the backend API received.
func (s *Server) Invitations() []Invitation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Invitation, 0, len(s.invites))
	for _, i := range s.invites {
		out = append(out, *i)
	}
	return out
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func (s *Server) serveJWKS(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.jwksHits++
	down := s.jwksDown
	s.mu.Unlock()
	if down {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	pub := s.key.PublicKey
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": KeyID,
		"n": b64(pub.N.Bytes()), "e": b64(big.NewInt(int64(pub.E)).Bytes()),
	}}})
}

func (s *Server) authorised(w http.ResponseWriter, r *http.Request) bool {
	s.mu.Lock()
	down := s.failAPI
	s.mu.Unlock()
	if down {
		http.Error(w, `{"errors":[{"code":"down"}]}`, http.StatusServiceUnavailable)
		return false
	}
	if r.Header.Get("Authorization") != "Bearer "+BackendKey() {
		http.Error(w, `{"errors":[{"code":"authentication_invalid"}]}`, http.StatusUnauthorized)
		return false
	}
	return true
}

func (s *Server) createInvitation(w http.ResponseWriter, r *http.Request) {
	if !s.authorised(w, r) {
		return
	}
	var body struct {
		EmailAddress string `json:"email_address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.EmailAddress == "" {
		http.Error(w, `{"errors":[{"code":"form_param_missing"}]}`, http.StatusUnprocessableEntity)
		return
	}
	s.mu.Lock()
	s.hasCreated++
	inv := &Invitation{ID: fmt.Sprintf("inv_mock_%d", s.hasCreated), Email: body.EmailAddress}
	s.invites = append(s.invites, inv)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "invitation", "id": inv.ID, "email_address": inv.Email, "status": "pending"})
}

func (s *Server) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	if !s.authorised(w, r) {
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	var found *Invitation
	for _, i := range s.invites {
		if i.ID == id {
			i.Revoked, found = true, i
		}
	}
	s.mu.Unlock()
	if found == nil {
		http.Error(w, `{"errors":[{"code":"resource_not_found"}]}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "invitation", "id": id, "status": "revoked"})
}

// Claims describes a session token. Zero values mean "valid": the mock issuer,
// an hour of life, a verified email.
type Claims struct {
	Subject       string
	Email         string
	Name          string
	ImageURL      string
	Unverified    bool          // email_verified=false
	Issuer        string        // override the iss
	ExpiresIn     time.Duration // default +1h; negative makes an expired token
	NotBeforeIn   time.Duration
	KeyID         string // override the kid header
	Alg           string // "RS256" (default) or "none"
	SignWith      *rsa.PrivateKey
	OmitSubject   bool
	TamperPayload bool // alter the payload after signing, keeping the signature
}

// Token signs a session token for c.
func (s *Server) Token(c Claims) string {
	now := time.Now()
	iss := c.Issuer
	if iss == "" {
		iss = s.Issuer()
	}
	exp := c.ExpiresIn
	if exp == 0 {
		exp = time.Hour
	}
	alg, kid, key := c.Alg, c.KeyID, c.SignWith
	if alg == "" {
		alg = "RS256"
	}
	if kid == "" {
		kid = KeyID
	}
	if key == nil {
		key = s.key
	}
	payload := map[string]any{
		"iss": iss, "iat": now.Unix(), "nbf": now.Add(c.NotBeforeIn).Unix() - 1, "exp": now.Add(exp).Unix(),
		"email": c.Email, "email_verified": !c.Unverified, "name": c.Name, "image_url": c.ImageURL,
	}
	if !c.OmitSubject {
		payload["sub"] = c.Subject
	}
	hb, _ := json.Marshal(map[string]string{"alg": alg, "typ": "JWT", "kid": kid})
	pb, _ := json.Marshal(payload)
	signing := b64(hb) + "." + b64(pb)
	if alg == "none" {
		return signing + "."
	}
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		panic("clerkmock: sign: " + err.Error())
	}
	if c.TamperPayload {
		payload["email"] = "attacker@example.test"
		pb, _ = json.Marshal(payload)
		signing = b64(hb) + "." + b64(pb)
	}
	return signing + "." + b64(sig)
}

// ForeignKey returns an RSA key the JWKS does not publish, for wrong-signature tests.
func ForeignKey() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		panic("clerkmock: key: " + err.Error())
	}
	return k
}

// SignWebhook returns the Svix headers for a message signed with the mock's secret.
func (s *Server) SignWebhook(msgID string, ts time.Time, payload []byte) http.Header {
	return SignWebhook(s.secret, msgID, ts, payload)
}

// SignWebhook signs payload with secret and returns the three Svix headers.
func SignWebhook(secret, msgID string, ts time.Time, payload []byte) http.Header {
	wh, err := svix.NewWebhook(secret)
	if err != nil {
		panic("clerkmock: secret: " + err.Error())
	}
	sig, _ := wh.Sign(msgID, ts, payload)
	h := http.Header{}
	h.Set("svix-id", msgID)
	h.Set("svix-timestamp", fmt.Sprint(ts.Unix()))
	h.Set("svix-signature", sig)
	h.Set("Content-Type", "application/json")
	return h
}
