// Package pgtest is the test seam for Postgres (rulings R6 and R7). Tests
// that need a database call DSN, which reads TEST_DATABASE_URL: unset means an
// explicit, visible SKIP locally and a hard FAIL under CI=true, so a database
// test can never be silently green. The DSN must point at loopback.
//
// NewDatabase (pgx-backed) creates a throwaway database per test on top of that
// seam; CheckLoopback and Reachable need no driver.
package pgtest
