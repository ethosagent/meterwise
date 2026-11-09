package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/nonet"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

func rawEnv(kv map[string]string) config.Lookup {
	return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
}

func env(kv map[string]string) config.Lookup { return rawEnv(kv) }

const goodDB = "postgres://mw:secret@127.0.0.1:5432/mw?sslmode=disable"

func TestLoadAppliesDefaults(t *testing.T) {
	c, err := config.Load(env(map[string]string{config.EnvDatabaseURL: goodDB}))
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenProxy != ":8080" || c.ListenAPI != ":8081" || c.ListenAdmin != "127.0.0.1:9090" {
		t.Fatalf("listen defaults wrong: %+v", c)
	}
	if c.AppName != "Meterwise" || c.ProxyPublicHost != "localhost:8080" || c.WebDir != "web/dist" {
		t.Fatalf("brand defaults wrong: %+v", c)
	}
	if !c.PrivacyMode || c.MaxWorkspaces != 20 || c.MaxConcurrentPerWorkspace != 8 || c.LogLevel != "info" || c.Env != "prod" {
		t.Fatalf("defaults wrong: %+v", c)
	}
}

func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	bad := map[string]string{
		config.EnvDatabaseURL:     "mysql://x",
		config.EnvListenProxy:     "nonsense",
		config.EnvListenAPI:       ":99999",
		config.EnvListenAdmin:     "",
		config.EnvProxyPublicHost: "https://proxy.example.com/path",
		config.EnvAppName:         "bad\x07name",
		config.EnvClerkIssuer:     "http://clerk.example.com",
		config.EnvClerkJWKSURL:    "ftp://x",
		config.EnvOTLPEndpoint:    "collector:4318",
		config.EnvPrivacyMode:     "maybe",
		config.EnvMaxWorkspaces:   "0",
		config.EnvMaxConcurrent:   "-3",
		config.EnvLogLevel:        "loud",
		config.EnvAppEnv:          "moon",
	}
	_, err := config.Load(env(bad))
	e := contract.AsError(err)
	if e.Code != contract.CodeConfigInvalid {
		t.Fatalf("code %s", e.Code)
	}
	// LISTEN_ADMIN is "" so it takes its (valid) default; every other key must be named.
	for key := range bad {
		if key == config.EnvListenAdmin {
			continue
		}
		if !hasDetail(e.Details, key) {
			t.Errorf("no problem reported for %s: %v", key, e.Details)
		}
	}
	if len(e.Details) != len(bad)-1 {
		t.Errorf("want %d problems, got %d: %v", len(bad)-1, len(e.Details), e.Details)
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	_, err := config.Load(env(nil))
	e := contract.AsError(err)
	if len(e.Details) != 1 || !hasDetail(e.Details, config.EnvDatabaseURL) {
		t.Fatalf("details: %v", e.Details)
	}
}

func TestListenersMustDiffer(t *testing.T) {
	_, err := config.Load(env(map[string]string{
		config.EnvDatabaseURL: goodDB, config.EnvListenProxy: ":9000", config.EnvListenAPI: "127.0.0.1:9000",
	}))
	e := contract.AsError(err)
	if len(e.Details) != 1 || !strings.Contains(e.Details[0], "already used by") {
		t.Fatalf("details: %v", e.Details)
	}
	// port 0 (ephemeral) may repeat: tests use it.
	if _, err := config.Load(env(map[string]string{
		config.EnvDatabaseURL: goodDB, config.EnvListenProxy: "127.0.0.1:0", config.EnvListenAPI: "127.0.0.1:0", config.EnvListenAdmin: "127.0.0.1:0",
	})); err != nil {
		t.Fatalf("ephemeral ports: %v", err)
	}
}

func TestValuesParsed(t *testing.T) {
	c, err := config.Load(env(map[string]string{
		config.EnvDatabaseURL: goodDB, config.EnvMaxWorkspaces: "7", config.EnvMaxConcurrent: "3",
		config.EnvPrivacyMode: "false", config.EnvAppName: "  Acme Meter ", config.EnvOTLPEndpoint: "http://localhost:4318",
		config.EnvLogLevel: "DEBUG", config.EnvAppEnv: "Prod",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxWorkspaces != 7 || c.MaxConcurrentPerWorkspace != 3 || c.PrivacyMode || c.AppName != "Acme Meter" ||
		c.OTLPEndpoint != "http://localhost:4318" || c.LogLevel != "debug" || c.Env != "prod" {
		t.Fatalf("got %+v", c)
	}
}

func TestRedactedHidesPassword(t *testing.T) {
	c, err := config.Load(env(map[string]string{config.EnvDatabaseURL: goodDB}))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range c.Redacted() {
		if strings.Contains(v, "secret") {
			t.Fatalf("%s leaks the password: %s", k, v)
		}
	}
	if !strings.Contains(c.Redacted()[config.EnvDatabaseURL], "xxxxx") {
		t.Fatalf("password not masked: %s", c.Redacted()[config.EnvDatabaseURL])
	}
}

func hasDetail(details []string, key string) bool {
	for _, d := range details {
		if strings.HasPrefix(d, key+":") {
			return true
		}
	}
	return false
}

func TestAppEnvDefaultsToProdWhenUnsetOrEmpty(t *testing.T) {
	cases := []struct {
		name string
		kv   map[string]string
		want string
	}{
		{"unset", map[string]string{}, "prod"},
		{"empty", map[string]string{config.EnvAppEnv: ""}, "prod"},
		{"dev", map[string]string{config.EnvAppEnv: "dev"}, "dev"},
		{"upper case", map[string]string{config.EnvAppEnv: "DEV"}, "dev"},
		{"test", map[string]string{config.EnvAppEnv: "test"}, "test"},
	}
	for _, tc := range cases {
		tc.kv[config.EnvDatabaseURL] = goodDB
		c, err := config.Load(env(tc.kv))
		if err != nil || c.Env != tc.want {
			t.Errorf("%s: env=%q err=%v, want %q", tc.name, c.Env, err, tc.want)
		}
	}
	if _, err := config.Load(env(map[string]string{config.EnvDatabaseURL: goodDB, config.EnvAppEnv: "staging"})); err == nil {
		t.Error("an unknown APP_ENV must be a config problem")
	}
}

func TestProdRequiresClerkSettingsButDevDoesNot(t *testing.T) {
	cfg, err := config.Load(rawEnv(map[string]string{config.EnvDatabaseURL: goodDB}))
	if err != nil {
		t.Fatalf("Load must not need identity settings (migrate uses it): %v", err)
	}
	e := contract.AsError(cfg.CheckServe())
	for _, key := range []string{config.EnvClerkIssuer, config.EnvClerkJWKSURL, config.EnvClerkSecretKey, config.EnvClerkWebhookKey} {
		if !strings.Contains(strings.Join(e.Details, "|"), key+": required") {
			t.Errorf("prod without %s was not reported: %v", key, e.Details)
		}
	}
	c, err := config.Load(rawEnv(map[string]string{config.EnvDatabaseURL: goodDB, config.EnvAppEnv: "dev"}))
	if err != nil || c.Clerk.Configured() || c.CheckServe() != nil {
		t.Fatalf("dev without Clerk: %+v %v", c.Clerk, err)
	}
}

func TestRedactedMasksEveryCredentialShape(t *testing.T) {
	dsns := []string{
		"postgres://mw:s3cr3tpw@127.0.0.1:5432/mw?sslmode=disable",
		"postgres://mw@127.0.0.1:5432/mw?password=s3cr3tpw",
		"postgres://mw@127.0.0.1:5432/mw?password=s3cr3%74pw&sslmode=disable",
		"postgres://mw@127.0.0.1:5432/mw?sslpassword=s3cr3tpw",
		"postgres://mw:p%40ss@127.0.0.1:5432/mw?passfile=s3cr3tpw",
	}
	for _, dsn := range dsns {
		c, err := config.Load(env(map[string]string{config.EnvDatabaseURL: dsn}))
		if err != nil {
			t.Fatal(err)
		}
		shown := c.Redacted()[config.EnvDatabaseURL]
		for _, leak := range []string{"s3cr3tpw", "s3cr3%74pw", "p%40ss", "p@ss"} {
			if strings.Contains(shown, leak) {
				t.Errorf("%s leaks %q: %s", dsn, leak, shown)
			}
		}
		if !strings.Contains(shown, "sslmode=disable") && strings.Contains(dsn, "sslmode") {
			t.Errorf("a harmless parameter was dropped: %s", shown)
		}
	}
	c, _ := config.Load(env(map[string]string{config.EnvDatabaseURL: goodDB, config.EnvClerkSecretKey: "sk_test_x", config.EnvClerkWebhookKey: "whsec_ZGVtbw=="}))
	if c.Redacted()[config.EnvClerkSecretKey] != "<set>" {
		t.Errorf("a set secret must show as <set>: %v", c.Redacted())
	}
	for k, v := range c.Redacted() {
		if strings.Contains(v, "sk_test_x") || strings.Contains(v, "whsec_") {
			t.Errorf("%s leaks a Clerk secret: %s", k, v)
		}
	}
}

func TestConfigCheckDoesNotPrintURLCredentials(t *testing.T) {
	c, err := config.Load(env(map[string]string{
		config.EnvDatabaseURL:     goodDB,
		config.EnvOTLPEndpoint:    "https://otelUser:otelPass@collector.example:4318/v1?api_key=otelKey",
		config.EnvClerkAPIURL:     "https://clerkTOKEN@api.clerk.test/v1",
		config.EnvClerkIssuer:     "https://issuerUser:issuerPass@clerk.example",
		config.EnvClerkJWKSURL:    "https://jwksTOKEN@clerk.example/.well-known/jwks.json",
		config.EnvAppURL:          "https://appUser:appPass@app.example",
		config.EnvClerkParties:    "https://app.example",
		config.EnvClerkSecretKey:  "sk_live_abc",
		config.EnvClerkWebhookKey: "whsec_ZGVtbw==",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range c.Redacted() {
		for _, leak := range []string{"otelUser", "otelPass", "otelKey", "clerkTOKEN", "issuerUser", "issuerPass", "jwksTOKEN", "appUser", "appPass", "sk_live", "whsec_"} {
			if strings.Contains(v, leak) {
				t.Errorf("%s leaks %q: %s", k, leak, v)
			}
		}
	}
	if got := c.Redacted()[config.EnvOTLPEndpoint]; !strings.Contains(got, "collector.example:4318") {
		t.Errorf("the host must stay visible: %s", got)
	}
}

func TestAuthorizedPartiesAreRequiredOutsideDevAndReportedWithTheOtherProblems(t *testing.T) {
	base := map[string]string{
		config.EnvDatabaseURL: goodDB, config.EnvClerkIssuer: "https://clerk.example", config.EnvClerkJWKSURL: "https://clerk.example/jwks",
		config.EnvClerkSecretKey: "sk_test_x", config.EnvClerkWebhookKey: "whsec_ZGVtbw==",
	}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	// prod: reported together with the other missing values
	cfg, err := config.Load(rawEnv(map[string]string{config.EnvDatabaseURL: goodDB}))
	if err != nil {
		t.Fatal(err)
	}
	if d := strings.Join(contract.AsError(cfg.CheckServe()).Details, "|"); !strings.Contains(d, config.EnvClerkParties+": required") || !strings.Contains(d, config.EnvClerkIssuer+": required") {
		t.Fatalf("prod: %s", d)
	}
	// test env with Clerk configured needs them; dev does not
	for env, wantErr := range map[string]bool{"prod": true, "test": true, "dev": false} {
		c, err := config.Load(rawEnv(with(map[string]string{config.EnvAppEnv: env})))
		if err != nil {
			t.Fatal(err)
		}
		if got := c.CheckServe() != nil; got != wantErr {
			t.Errorf("APP_ENV=%s without parties: error=%v, want %v", env, got, wantErr)
		}
	}
	ok, err := config.Load(rawEnv(with(map[string]string{config.EnvAppEnv: "prod", config.EnvClerkParties: "https://app.example, https://admin.example/"})))
	if err != nil || ok.CheckServe() != nil || len(ok.Clerk.AuthorizedParties) != 2 || ok.Clerk.AuthorizedParties[1] != "https://admin.example" {
		t.Fatalf("%+v %v %v", ok.Clerk.AuthorizedParties, err, ok.CheckServe())
	}
	for _, bad := range []string{"app.example", "https://app.example/path", "ftp://app.example", "https://u:p@app.example",
		"https://app.example#frag", "https://app.example/#frag", "https://*.example.com", "https://*", "https://app.*.example",
		"https://", "https://app.example:99999", "https://app example", "https://app.example:", "http://*:3000"} {
		_, err := config.Load(rawEnv(with(map[string]string{config.EnvClerkParties: bad})))
		if e := contract.AsError(err); e.Code != contract.CodeConfigInvalid || !hasDetail(e.Details, config.EnvClerkParties) {
			t.Errorf("%q accepted: %v", bad, err)
		}
	}
}
