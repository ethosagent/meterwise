package config

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Memory environment variables (plan 09, rulings R2 and R14). They are the only source of memory's own model key.
const (
	EnvMemoryAuth           = "MEMORY_API_KEY"
	EnvMemoryBaseURL        = "MEMORY_BASE_URL"
	EnvMemoryModel          = "MEMORY_MODEL"
	EnvMemoryEmbedModel     = "MEMORY_EMBED_MODEL"
	EnvMemorySelfBudgetUSD  = "MEMORY_SELF_BUDGET_USD"
	EnvMemoryRetentionDays  = "MEMORY_TRACE_RETENTION_DAYS"
	DefaultMemoryBaseURL    = "https://api.openai.com/v1"
	DefaultMemoryModel      = "gpt-4o-mini"
	DefaultMemoryEmbedModel = "text-embedding-3-small"
	DefaultMemoryBudgetUSD  = "1"
	DefaultMemoryRetention  = 90
	maxMemoryModelLen       = 128
)

// Memory is the trace library's own model access: our key, our endpoint, our budget. The key is used only by
// internal/memory's http.Client, is never logged and never shown (Redacted prints set or unset). Without a key,
// memory is off: no capture, no recall, and /statusz and the settings say so.
type Memory struct {
	APIKey        string // secret; empty = memory disabled (disabledReason no_key)
	BaseURL       string // an OpenAI-compatible API root, without a trailing slash
	Model         string // the distil model
	EmbedModel    string // the embedding model (1536 dimensions)
	SelfBudget    contract.MicroUSD
	RetentionDays int
}

// Enabled reports whether memory has a key and may call a model at all.
func (m Memory) Enabled() bool { return m.APIKey != "" }

func loadMemory(get Lookup, p *problems) Memory {
	m := Memory{
		APIKey:        str(get, EnvMemoryAuth, ""),
		BaseURL:       strings.TrimRight(str(get, EnvMemoryBaseURL, DefaultMemoryBaseURL), "/"),
		Model:         strings.TrimSpace(str(get, EnvMemoryModel, DefaultMemoryModel)),
		EmbedModel:    strings.TrimSpace(str(get, EnvMemoryEmbedModel, DefaultMemoryEmbedModel)),
		RetentionDays: positive(get, EnvMemoryRetentionDays, DefaultMemoryRetention, p),
	}
	if !hasScheme(m.BaseURL, "http", "https") || strings.ContainsAny(m.BaseURL, "?#") {
		p.add(EnvMemoryBaseURL, "must be an http(s):// URL with a host and no query (example https://api.openai.com/v1)")
	} else if u, err := url.Parse(m.BaseURL); err != nil || u.User != nil {
		p.add(EnvMemoryBaseURL, "must not carry credentials; the key is MEMORY_API_KEY")
	}
	for _, f := range []struct{ key, val string }{{EnvMemoryModel, m.Model}, {EnvMemoryEmbedModel, m.EmbedModel}} {
		if f.val == "" || len(f.val) > maxMemoryModelLen || strings.IndexFunc(f.val, unicode.IsControl) >= 0 {
			p.add(f.key, "must be 1-128 characters with no control characters")
		}
	}
	budget := str(get, EnvMemorySelfBudgetUSD, DefaultMemoryBudgetUSD)
	b, err := contract.ParseMicroUSD(budget)
	if err != nil || b <= 0 {
		p.add(EnvMemorySelfBudgetUSD, budget+" is not a positive USD amount with at most 6 decimals (example 1 or 0.25)")
	} else {
		m.SelfBudget = b
	}
	return m
}

// redacted adds memory's settings to the display map: the key as set or unset, never its value.
func (m Memory) redacted(out map[string]string) {
	out[EnvMemoryAuth] = "unset"
	if m.APIKey != "" {
		out[EnvMemoryAuth] = "set"
	}
	out[EnvMemoryBaseURL] = redactURL(m.BaseURL)
	out[EnvMemoryModel] = m.Model
	out[EnvMemoryEmbedModel] = m.EmbedModel
	out[EnvMemorySelfBudgetUSD] = m.SelfBudget.Decimal()
	out[EnvMemoryRetentionDays] = strconv.Itoa(m.RetentionDays)
}
