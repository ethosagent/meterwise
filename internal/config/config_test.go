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
	if c.Listen != ":8080" || c.LogLevel != "info" || c.Env != "dev" || c.MaxWorkspaces != 20 {
		t.Fatalf("defaults wrong: %+v", c)
	}
}

func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	_, err := config.Load(env(map[string]string{
		config.EnvListen:       "nonsense",
		config.EnvLogLevel:     "loud",
		config.EnvEnv:          "moon",
		config.EnvMaxWorkspace: "0",
		config.EnvDatabaseURL:  "mysql://x",
	}))
	e := contract.AsError(err)
	if e.Code != contract.CodeConfigInvalid {
		t.Fatalf("code %s", e.Code)
	}
	for _, key := range []string{config.EnvListen, config.EnvLogLevel, config.EnvEnv, config.EnvMaxWorkspace, config.EnvDatabaseURL} {
		if !hasDetail(e.Details, key) {
			t.Errorf("no problem reported for %s: %v", key, e.Details)
		}
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	_, err := config.Load(env(nil))
	e := contract.AsError(err)
	if len(e.Details) != 1 || !hasDetail(e.Details, config.EnvDatabaseURL) {
		t.Fatalf("details: %v", e.Details)
	}
}

func TestMaxWorkspacesParsed(t *testing.T) {
	c, err := config.Load(env(map[string]string{config.EnvDatabaseURL: goodDB, config.EnvMaxWorkspace: "7"}))
	if err != nil || c.MaxWorkspaces != 7 {
		t.Fatalf("got %+v, %v", c, err)
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
