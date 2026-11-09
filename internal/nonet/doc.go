// Package nonet is the test-time network guard (ruling R6): "no network" means
// no external network, loopback stays open (httptest, Postgres). A package's
// TestMain calls Run, after which dials to non-loopback addresses and DNS
// lookups fail with ErrBlocked instead of silently reaching the internet.
//
// Known limit: the guard covers DNS and http.DefaultTransport. A direct dial to a literal
// external IP (net.Dial("tcp", "1.1.1.1:443")) is not intercepted in-process; the kernel-level
// backstop is the CI job that runs the tests with --network none. Hostnames, including through
// a custom http.Transport, fail at the resolver.
//
// It is test support: only _test.go files and test-seam packages such as
// store/pgtest use it.
package nonet
