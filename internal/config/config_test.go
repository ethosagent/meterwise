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

func env(kv map[string]string) config.Lookup {
	return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
}

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
	if !c.PrivacyMode || c.MaxWorkspaces != 20 || c.MaxConcurrentPerWorkspace != 8 || c.LogLevel != "info" || c.Env != "dev" {
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
