// Package config turns environment variables into a validated Config. It
// never reads the process environment itself: the caller injects a lookup
// function (cmd/ owns os.Getenv), which keeps it testable and keeps the
// forbidigo lint rule meaningful. Validation reports every problem at once
// (`meterwised config check -json`), never just the first.
package config
