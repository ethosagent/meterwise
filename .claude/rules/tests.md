---
paths:
  - "**/*_test.go"
  - "web/src/**/*.test.ts"
  - "web/src/**/*.test.tsx"
---

# Test rules

Checklist: `.agents/reviews/test-quality.md` (TEST-*).

- Write the test first and see it fail for the right reason.
- Table-driven with `t.Run`; case names read as behaviour.
- Every Go test package has `TestMain` calling `nonet.Run`: loopback only, no external network, no real keys.
- Adapters have a mock twin with committed fixtures (`clerkmock`, `pgtest`, `testkit.FakeUpstream`). Database tests go through `pgtest.DSN`.
- A skipped test prints `! SKIP` in the gate; never add a skip to go green.
- No `time.Sleep` for ordering; poll with a deadline. Inject clocks.
- Assert at the boundary: status, body shape, error code, rows written, hook calls received.
- Cover failure paths: errors, timeouts, overflow, cancel, bad input.
- Never weaken a test or lower `COVERAGE_MIN` to go green. Test files stay under 500 lines.
- Run quietly: `scripts/quiet.sh go test -race ./pkg` or `scripts/run-checks.sh test`.
