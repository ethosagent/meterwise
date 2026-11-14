package config_test

import (
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
)

func memEnv(kv map[string]string) config.Lookup {
	kv[config.EnvDatabaseURL] = goodDB
	return env(kv)
}

func TestMemoryDefaultsAndNoKeyMeansDisabled(t *testing.T) {
	c, err := config.Load(memEnv(map[string]string{}))
	if err != nil {
		t.Fatal(err)
	}
	m := c.Memory
	if m.Enabled() || m.APIKey != "" || m.BaseURL != "https://api.openai.com/v1" || m.Model != "gpt-4o-mini" || m.EmbedModel != "text-embedding-3-small" ||
		m.SelfBudget != contract.MicroUSD(1_000_000) || m.RetentionDays != 90 {
		t.Fatalf("memory defaults = %+v", m)
	}
}

func TestMemoryReadsItsSettingsAndNeverShowsTheKey(t *testing.T) {
	c, err := config.Load(memEnv(map[string]string{
		config.EnvMemoryAuth: "sk-super-secret-1234", config.EnvMemoryBaseURL: "http://127.0.0.1:9999/v1/", config.EnvMemoryModel: "m1",
		config.EnvMemoryEmbedModel: "e1", config.EnvMemorySelfBudgetUSD: "0.25", config.EnvMemoryRetentionDays: "30",
	}))
	if err != nil {
		t.Fatal(err)
	}
	m := c.Memory
	if !m.Enabled() || m.BaseURL != "http://127.0.0.1:9999/v1" || m.Model != "m1" || m.EmbedModel != "e1" || m.SelfBudget != 250_000 || m.RetentionDays != 30 {
		t.Fatalf("memory = %+v", m)
	}
	shown := c.Redacted()
	if shown[config.EnvMemoryAuth] != "set" {
		t.Fatalf("the key must show as set, got %q", shown[config.EnvMemoryAuth])
	}
	for k, v := range shown {
		if strings.Contains(v, "super-secret") {
			t.Fatalf("%s leaks the key: %q", k, v)
		}
	}
}

func TestMemoryProblemsAreListedWithTheRestAtOnce(t *testing.T) {
	// MEMORY_MODEL is "" so it takes its (valid) default; every other key must be named.
	_, err := config.Load(memEnv(map[string]string{
		config.EnvMemoryBaseURL: "ftp://x", config.EnvMemoryEmbedModel: strings.Repeat("x", 200),
		config.EnvMemorySelfBudgetUSD: "-1", config.EnvMemoryRetentionDays: "0",
	}))
	e := contract.AsError(err)
	if e.Code != contract.CodeConfigInvalid {
		t.Fatalf("code %s", e.Code)
	}
	for _, k := range []string{config.EnvMemoryBaseURL, config.EnvMemoryEmbedModel, config.EnvMemorySelfBudgetUSD, config.EnvMemoryRetentionDays} {
		if !hasDetail(e.Details, k) {
			t.Errorf("no problem reported for %s: %v", k, e.Details)
		}
	}
	_, err = config.Load(memEnv(map[string]string{config.EnvMemoryBaseURL: "https://user:pw@host/v1"}))
	if e := contract.AsError(err); e.Code != contract.CodeConfigInvalid || !strings.Contains(strings.Join(e.Details, "|"), "credentials") {
		t.Fatalf("credentials in the URL must be refused: %v", err)
	}
}
