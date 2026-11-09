package clerkmock_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/identity/clerkmock"
	"github.com/ethosagent/meterwise/internal/nonet"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

func get(t *testing.T, url string, hdr map[string]string, method string, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func TestJWKSAndOutage(t *testing.T) {
	s, err := clerkmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	res := get(t, s.JWKSURL(), nil, http.MethodGet, "")
	var doc struct{ Keys []map[string]string }
	if err := json.NewDecoder(res.Body).Decode(&doc); err != nil || len(doc.Keys) != 1 || doc.Keys[0]["kid"] != clerkmock.KeyID || doc.Keys[0]["n"] == "" {
		t.Fatalf("jwks: %+v %v", doc, err)
	}
	s.SetJWKSDown(true)
	if res := get(t, s.JWKSURL(), nil, http.MethodGet, ""); res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("down: %d", res.StatusCode)
	}
	if s.JWKSHits() != 2 {
		t.Fatalf("hits=%d", s.JWKSHits())
	}
	if s.Issuer() == "" || !strings.HasSuffix(s.APIURL(), "/v1") || !strings.HasPrefix(s.WebhookSecret(), "whsec_") {
		t.Fatal("endpoint accessors")
	}
}

func TestBackendAPI(t *testing.T) {
	s, _ := clerkmock.New()
	defer s.Close()
	auth := map[string]string{"Authorization": "Bearer " + clerkmock.BackendKey()}
	if res := get(t, s.APIURL()+"/invitations", nil, http.MethodPost, `{"email_address":"a@x.test"}`); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no key: %d", res.StatusCode)
	}
	if res := get(t, s.APIURL()+"/invitations", auth, http.MethodPost, `{}`); res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("no email: %d", res.StatusCode)
	}
	res := get(t, s.APIURL()+"/invitations", auth, http.MethodPost, `{"email_address":"a@x.test"}`)
	var inv struct{ ID string }
	if err := json.NewDecoder(res.Body).Decode(&inv); err != nil || res.StatusCode != http.StatusOK || inv.ID == "" {
		t.Fatalf("create: %d %+v %v", res.StatusCode, inv, err)
	}
	if res := get(t, s.APIURL()+"/invitations/nope/revoke", auth, http.MethodPost, ""); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown revoke: %d", res.StatusCode)
	}
	if res := get(t, s.APIURL()+"/invitations/"+inv.ID+"/revoke", auth, http.MethodPost, ""); res.StatusCode != http.StatusOK {
		t.Fatalf("revoke: %d", res.StatusCode)
	}
	if got := s.Invitations(); len(got) != 1 || !got[0].Revoked || got[0].Email != "a@x.test" {
		t.Fatalf("recorded: %+v", got)
	}
	s.SetAPIDown(true)
	if res := get(t, s.APIURL()+"/invitations", auth, http.MethodPost, `{"email_address":"b@x.test"}`); res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("api down: %d", res.StatusCode)
	}
}

func decode(t *testing.T, tok string) (header, payload map[string]any, parts int) {
	t.Helper()
	ps := strings.Split(tok, ".")
	dec := func(p string) map[string]any {
		var m map[string]any
		b, err := base64URL(p)
		if err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(b, &m)
		return m
	}
	return dec(ps[0]), dec(ps[1]), len(ps)
}

func TestTokenForgeOptions(t *testing.T) {
	s, _ := clerkmock.New()
	defer s.Close()
	h, p, _ := decode(t, s.Token(clerkmock.Claims{Subject: "user_1", Email: "a@x.test", Name: "Ada", ImageURL: "i.png"}))
	if h["alg"] != "RS256" || h["kid"] != clerkmock.KeyID || p["sub"] != "user_1" || p["iss"] != s.Issuer() || p["email_verified"] != true || p["name"] != "Ada" {
		t.Fatalf("valid token: %v %v", h, p)
	}
	_, p, _ = decode(t, s.Token(clerkmock.Claims{Subject: "u", Unverified: true, Issuer: "https://evil.test", OmitSubject: true}))
	if p["email_verified"] != false || p["iss"] != "https://evil.test" || p["sub"] != nil {
		t.Fatalf("forged claims: %v", p)
	}
	h, _, n := decode(t, s.Token(clerkmock.Claims{Subject: "u", Alg: "none", KeyID: "other"}))
	if h["alg"] != "none" || h["kid"] != "other" || n != 3 {
		t.Fatalf("alg none: %v %d", h, n)
	}
	valid := s.Token(clerkmock.Claims{Subject: "u", Email: "a@x.test"})
	tampered := s.Token(clerkmock.Claims{Subject: "u", Email: "a@x.test", TamperPayload: true})
	if strings.Split(valid, ".")[1] == strings.Split(tampered, ".")[1] {
		t.Fatal("TamperPayload did not change the payload")
	}
	if foreign := s.Token(clerkmock.Claims{Subject: "u", SignWith: clerkmock.ForeignKey(), ExpiresIn: -time.Hour}); foreign == valid {
		t.Fatal("foreign key token equals the valid one")
	}
}

func TestFixturesAndWebhookSigning(t *testing.T) {
	names := clerkmock.FixtureNames()
	if len(names) != 3 {
		t.Fatalf("fixtures: %v", names)
	}
	for _, n := range names {
		b, err := clerkmock.Fixture(n)
		if err != nil || !json.Valid(b) {
			t.Fatalf("fixture %s: %v", n, err)
		}
	}
	if _, err := clerkmock.Fixture("nope"); err == nil {
		t.Fatal("an unknown fixture must error")
	}
	s, _ := clerkmock.New()
	defer s.Close()
	h := s.SignWebhook("msg_1", time.Unix(1700000000, 0), []byte("{}"))
	if h.Get("svix-id") != "msg_1" || h.Get("svix-timestamp") != "1700000000" || !strings.HasPrefix(h.Get("svix-signature"), "v1,") {
		t.Fatalf("headers: %v", h)
	}
}

func base64URL(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }
