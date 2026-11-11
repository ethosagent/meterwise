// Package trim rewrites a request's tool results so the model sees the signal and the customer pays for fewer
// input tokens (plan 11). It is a pure, I/O-free tollgate Hook (Name "trim") with five stages behind one Stage
// interface: repeat_collapse, log_window, dup_lines, json_table and, only in aggressive mode, schema_trim.
//
// Trimming is deterministic and idempotent (rulings R6, A5): the output for a tool-result region is a pure function
// of the region's bytes, the earlier messages and the workspace config. The hook decides each region once per
// session, keyed by the hash of its content, and keeps the decision in a bounded memo, so every later call sends the
// same bytes for it and a provider's prefix cache stays warm. A region it did not decide before its deadline stays
// unchanged. It edits the body only through internal/wire's byte splice, never changes the system prompt or the tool
// definitions in lossless mode, and reports what it did in Meta[contract.MetaTrim]; the ledger prices it. It imports
// only the standard library, tollgate's root, internal/contract and internal/wire (rule H3).
package trim
