// Package nonet is the test-time network guard (ruling R6): "no network" means
// no external network, loopback stays open (httptest, Postgres). A package's
// TestMain calls Run, after which dials to non-loopback addresses and DNS
// lookups fail with ErrBlocked instead of silently reaching the internet.
//
// It is test support: only _test.go files and test-seam packages such as
// store/pgtest use it.
package nonet
