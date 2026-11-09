package config

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Lookup returns the value of an environment variable and whether it is set.
type Lookup func(key string) (string, bool)

// Environment variable names. One place, so docs and errors cannot drift.
const (
	EnvDatabaseURL     = "DATABASE_URL"
	EnvListenProxy     = "LISTEN_PROXY"
	EnvListenAPI       = "LISTEN_API"
	EnvListenAdmin     = "LISTEN_ADMIN"
	EnvProxyPublicHost = "PROXY_PUBLIC_HOST"
	EnvAppName         = "APP_NAME"
	EnvClerkIssuer     = "CLERK_ISSUER"
	EnvClerkJWKSURL    = "CLERK_JWKS_URL"
	EnvClerkPubKey     = "CLERK_PUBLISHABLE_KEY"
	EnvOTLPEndpoint    = "OTEL_EXPORTER_OTLP_ENDPOINT"
	EnvPrivacyMode     = "PRIVACY_MODE"
	EnvMaxWorkspaces   = "MAX_WORKSPACES"
	EnvMaxConcurrent   = "MAX_CONCURRENT_PER_WORKSPACE"
	EnvWebDir          = "WEB_DIR"
	EnvLogLevel        = "LOG_LEVEL"
	EnvAppEnv          = "APP_ENV"
)

// Clerk holds the identity-provider settings Plan 02 consumes. Plan 01 only
// validates their shape; none is required yet.
type Clerk struct {
	Issuer         string
	JWKSURL        string
	PublishableKey string
}

// Config is the validated process configuration.
type Config struct {
	DatabaseURL               string // credentials-bearing; use Redacted for display
	ListenProxy               string // the data plane (tollgate), public
	ListenAPI                 string // the control plane and the web app
	ListenAdmin               string // tollgate /statusz and /metrics; keep private
	ProxyPublicHost           string // host agents use to reach the proxy, shown in Setup
	AppName                   string // brand shown by the shell
	Clerk                     Clerk
	OTLPEndpoint              string // empty: telemetry off
	PrivacyMode               bool   // default true (decision D12)
	MaxWorkspaces             int
	MaxConcurrentPerWorkspace int
	WebDir                    string // built web app, served on ListenAPI
	LogLevel                  string
	Env                       string
}

// Defaults applied when a variable is unset.
const (
	DefaultListenProxy               = ":8080"
	DefaultListenAPI                 = ":8081"
	DefaultListenAdmin               = "127.0.0.1:9090"
	DefaultProxyPublicHost           = "localhost:8080"
	DefaultAppName                   = "Meterwise"
	DefaultWebDir                    = "web/dist"
	DefaultLogLevel                  = "info"
	DefaultEnv                       = "dev"
	DefaultMaxWorkspaces             = 20
	DefaultMaxConcurrentPerWorkspace = 8
	maxAppNameLen                    = 64
)

var (
	logLevels = []string{"debug", "info", "warn", "error"}
	envNames  = []string{"dev", "test", "prod"}
)

// problems collects every validation failure so Load can report them at once.
type problems []string

func (p *problems) add(key, msg string) { *p = append(*p, key+": "+msg) }

// Load reads and validates the configuration. On failure the returned error is
// a *contract.Error with CodeConfigInvalid whose Details list every problem.
func Load(get Lookup) (Config, error) {
	var p problems
	c := Config{
		DatabaseURL:     str(get, EnvDatabaseURL, ""),
		ListenProxy:     str(get, EnvListenProxy, DefaultListenProxy),
		ListenAPI:       str(get, EnvListenAPI, DefaultListenAPI),
		ListenAdmin:     str(get, EnvListenAdmin, DefaultListenAdmin),
		ProxyPublicHost: str(get, EnvProxyPublicHost, DefaultProxyPublicHost),
		AppName:         strings.TrimSpace(str(get, EnvAppName, DefaultAppName)),
		Clerk: Clerk{
			Issuer:         str(get, EnvClerkIssuer, ""),
			JWKSURL:        str(get, EnvClerkJWKSURL, ""),
			PublishableKey: str(get, EnvClerkPubKey, ""),
		},
		OTLPEndpoint: str(get, EnvOTLPEndpoint, ""),
		WebDir:       str(get, EnvWebDir, DefaultWebDir),
		LogLevel:     strings.ToLower(str(get, EnvLogLevel, DefaultLogLevel)),
		Env:          strings.ToLower(str(get, EnvAppEnv, DefaultEnv)),
	}
	c.PrivacyMode = boolean(get, EnvPrivacyMode, true, &p)
	c.MaxWorkspaces = positive(get, EnvMaxWorkspaces, DefaultMaxWorkspaces, &p)
	c.MaxConcurrentPerWorkspace = positive(get, EnvMaxConcurrent, DefaultMaxConcurrentPerWorkspace, &p)

	c.validateListeners(&p)
	c.validateIdentity(&p)
	if !oneOf(c.LogLevel, logLevels) {
		p.add(EnvLogLevel, c.LogLevel+" is not one of "+strings.Join(logLevels, "|"))
	}
	if !oneOf(c.Env, envNames) {
		p.add(EnvAppEnv, c.Env+" is not one of "+strings.Join(envNames, "|"))
	}
	if c.DatabaseURL == "" {
		p.add(EnvDatabaseURL, "required (postgres://user:pass@host:5432/db)")
	} else if !hasScheme(c.DatabaseURL, "postgres", "postgresql") {
		p.add(EnvDatabaseURL, "must be a postgres:// URL with a host")
	}
	if c.OTLPEndpoint != "" && !hasScheme(c.OTLPEndpoint, "http", "https") {
		p.add(EnvOTLPEndpoint, "must be an http(s):// URL with a host (example http://localhost:4318)")
	}

	if len(p) > 0 {
		return Config{}, contract.NewError(contract.CodeConfigInvalid, "configuration is invalid", p...)
	}
	return c, nil
}

func (c Config) validateListeners(p *problems) {
	seen := map[string]string{}
	for _, l := range []struct{ key, val string }{
		{EnvListenProxy, c.ListenProxy}, {EnvListenAPI, c.ListenAPI}, {EnvListenAdmin, c.ListenAdmin},
	} {
		_, port, err := net.SplitHostPort(l.val)
		n, perr := strconv.Atoi(port)
		if err != nil || perr != nil || n < 0 || n > 65535 {
			p.add(l.key, l.val+" is not host:port (example :8080)")
			continue
		}
		if prev, dup := seen[port]; dup && n != 0 {
			p.add(l.key, "port "+port+" is already used by "+prev)
		}
		seen[port] = l.key
	}
	if h := c.ProxyPublicHost; h == "" || strings.ContainsAny(h, "/?# \t") || strings.Contains(h, "://") {
		p.add(EnvProxyPublicHost, h+" must be a bare host or host:port, no scheme or path (example proxy.example.com)")
	}
	if n := len([]rune(c.AppName)); n == 0 || n > maxAppNameLen || strings.IndexFunc(c.AppName, unicode.IsControl) >= 0 {
		p.add(EnvAppName, "must be 1-64 characters with no control characters")
	}
}

func (c Config) validateIdentity(p *problems) {
	if c.Clerk.Issuer != "" && !hasScheme(c.Clerk.Issuer, "https") {
		p.add(EnvClerkIssuer, "must be an https:// URL")
	}
	if c.Clerk.JWKSURL != "" && !hasScheme(c.Clerk.JWKSURL, "https") {
		p.add(EnvClerkJWKSURL, "must be an https:// URL")
	}
}

// Redacted returns the config as display-safe key/values: the database URL has
// its password replaced, so `config check` output can be pasted in a ticket.
func (c Config) Redacted() map[string]string {
	db := c.DatabaseURL
	if u, err := url.Parse(db); err == nil && u.User != nil {
		db = u.Redacted()
	}
	return map[string]string{
		EnvDatabaseURL:     db,
		EnvListenProxy:     c.ListenProxy,
		EnvListenAPI:       c.ListenAPI,
		EnvListenAdmin:     c.ListenAdmin,
		EnvProxyPublicHost: c.ProxyPublicHost,
		EnvAppName:         c.AppName,
		EnvClerkIssuer:     c.Clerk.Issuer,
		EnvClerkJWKSURL:    c.Clerk.JWKSURL,
		EnvClerkPubKey:     c.Clerk.PublishableKey,
		EnvOTLPEndpoint:    c.OTLPEndpoint,
		EnvPrivacyMode:     strconv.FormatBool(c.PrivacyMode),
		EnvMaxWorkspaces:   strconv.Itoa(c.MaxWorkspaces),
		EnvMaxConcurrent:   strconv.Itoa(c.MaxConcurrentPerWorkspace),
		EnvWebDir:          c.WebDir,
		EnvLogLevel:        c.LogLevel,
		EnvAppEnv:          c.Env,
	}
}

func str(get Lookup, key, def string) string {
	if v, ok := get(key); ok && v != "" {
		return v
	}
	return def
}

func boolean(get Lookup, key string, def bool, p *problems) bool {
	raw, ok := get(key)
	if !ok || raw == "" {
		return def
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		p.add(key, raw+" is not a boolean (true|false)")
		return def
	}
	return b
}

func positive(get Lookup, key string, def int, p *problems) int {
	raw, ok := get(key)
	if !ok || raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		p.add(key, "must be an integer >= 1")
		return def
	}
	return n
}

func hasScheme(raw string, schemes ...string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && oneOf(u.Scheme, schemes)
}

func oneOf(v string, set []string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}
