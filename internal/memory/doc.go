// Package memory is the trace library (Plan 09): when a session finishes with a solution it distils that solution into a
// small trace, embeds it with our own model key, stores it per workspace (pgvector), and on a later session that looks
// alike injects one "[proxy] memory:" hint line. The saving is likely, never certain, and never enters the certain
// number (ADR 0027).
//
// Three rules shape it. First, recall never touches the request path: Before only reads a result the session's worker
// already cached and, in hint mode, splices one note (internal/wire); the query embedding and the pgvector lookup run
// in a bounded per-workspace worker started from call 1's After (ADR 0031, ruling R1). Second, the key is ours: the one
// model key comes from MEMORY_* through this package's own http.Client, with a daily budget per workspace; it is never
// logged and never reaches a DTO (ADR 0033, archtest H12). Third, privacy wins: capture and recall run only where the
// ledger's capture rule allows text for the workspace (non-negotiable 3); everything stored passes a redaction pass.
//
// memory is rank 3 and not pure: it may import config, contract, store, wire, wsauth and tollgate's root; it never
// imports the ledger (the ledger reads Meta["memory"], and the host connects the two through small ports).
package memory
