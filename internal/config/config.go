package config

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Lookup returns the value of an environment variable and whether it is set.
type Lookup func(key string) (string, bool)

// Environment variable names. One place, so docs and errors cannot drift.
const (
	EnvListen       = "METERWISE_LISTEN"
	EnvDatabaseURL  = "METERWISE_DATABASE_URL"
	EnvLogLevel     = "METERWISE_LOG_LEVEL"
	EnvEnv          = "METERWISE_ENV"
	EnvMaxWorkspace = "METERWISE_MAX_WORKSPACES"
)

// Config is the validated process configuration.
type Config struct {
	Listen        string
	DatabaseURL   string // credentials-bearing; use Redacted for display
	LogLevel      string
	Env           string
	MaxWorkspaces int
}

// Defaults applied when a variable is unset.
const (
	DefaultListen        = ":8080"
	DefaultLogLevel      = "info"
	DefaultEnv           = "dev"
	DefaultMaxWorkspaces = 20
)

var (
	logLevels = []string{"debug", "info", "warn", "error"}
	envNames  = []string{"dev", "test", "prod"}
)

// Load reads and validates the configuration. On failure the returned error is
// a *contract.Error with CodeConfigInvalid whose Details list every problem.
func Load(get Lookup) (Config, error) {
	c := Config{
		Listen:        str(get, EnvListen, DefaultListen),
		DatabaseURL:   str(get, EnvDatabaseURL, ""),
		LogLevel:      strings.ToLower(str(get, EnvLogLevel, DefaultLogLevel)),
		Env:           strings.ToLower(str(get, EnvEnv, DefaultEnv)),
		MaxWorkspaces: DefaultMaxWorkspaces,
	}
	var problems []string
	add := func(p string) { problems = append(problems, p) }

	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		add(EnvListen + ": " + c.Listen + " is not host:port (example :8080)")
	}
	if !oneOf(c.LogLevel, logLevels) {
		add(EnvLogLevel + ": " + c.LogLevel + " is not one of " + strings.Join(logLevels, "|"))
	}
	if !oneOf(c.Env, envNames) {
		add(EnvEnv + ": " + c.Env + " is not one of " + strings.Join(envNames, "|"))
	}
	if raw, ok := get(EnvMaxWorkspace); ok && raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			add(EnvMaxWorkspace + ": must be an integer >= 1")
		} else {
			c.MaxWorkspaces = n
		}
	}
	if c.DatabaseURL == "" {
		add(EnvDatabaseURL + ": required (postgres://user:pass@host:5432/db)")
	} else if u, err := url.Parse(c.DatabaseURL); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		add(EnvDatabaseURL + ": must be a postgres:// URL with a host")
	}

	if len(problems) > 0 {
		return Config{}, contract.NewError(contract.CodeConfigInvalid, "configuration is invalid", problems...)
	}
	return c, nil
}

// Redacted returns the config as display-safe key/values: the database URL has
// its password replaced, so `config check` output can be pasted in a ticket.
func (c Config) Redacted() map[string]string {
	db := c.DatabaseURL
	if u, err := url.Parse(db); err == nil && u.User != nil {
		db = u.Redacted()
	}
	return map[string]string{
		EnvListen:       c.Listen,
		EnvDatabaseURL:  db,
		EnvLogLevel:     c.LogLevel,
		EnvEnv:          c.Env,
		EnvMaxWorkspace: strconv.Itoa(c.MaxWorkspaces),
	}
}

func str(get Lookup, key, def string) string {
	if v, ok := get(key); ok && v != "" {
		return v
	}
	return def
}

func oneOf(v string, set []string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}
