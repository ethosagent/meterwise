// Package pgtest is the test seam for Postgres (rulings R6 and R7). Tests
// that need a database call DSN, which reads TEST_DATABASE_URL: unset means an
// explicit, visible SKIP locally and a hard FAIL under CI=true, so a database
// test can never be silently green. The DSN must point at loopback.
//
// Plan 01 adds the pgx-backed Open and per-test schema on top of this seam;
// 00a ships only what needs no driver.
package pgtest
