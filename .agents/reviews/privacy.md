# Privacy (`PRIV-*`)

Guards non-negotiables 4 and 5 in `CLAUDE.md` and decision D12.

## Already covered by CI

- Lint bans `log.Print*` and `fmt.Print*` (all logs go through `slog`).
- `config check` prints the database URL with the password masked.

## Checks

**PRIV-1 Logs carry metadata, not content.** No request or response body, message text,
reasoning, tool arguments or raw SSE bytes in any log line at any level, whatever the
workspace's privacy setting. Log lines carry ids (workspace, session, call), provider, model.

**PRIV-2 Credentials are never logged or stored.** `Authorization`, `x-api-key`, Clerk
tokens, provider keys and any secret-looking header are redacted before they reach a
logger, error, table or metric. No provider key is ever persisted (decision D7).

**PRIV-3 Upstream credentials pass through untouched.** The host never reads, rewrites,
caches or stores them; only tollgate's transport sends them.

**PRIV-4 Tables hold metadata unless capture is on.** `calls` and `sessions` store
counts, tokens, cost, hashes and flags. Bodies exist only for a workspace that opted in to
capture, size-capped, in a table built for it.

**PRIV-5 Errors leak nothing.** Error bodies carry a registry code and a safe message:
no stack, SQL, internal address or other workspace's data (`contract.AsError` enforces this).

**PRIV-6 Workspace ids are secrets-lite.** The public id in `/w/{id}` never appears in
logs, metrics labels or error bodies; log the internal workspace id instead.

**PRIV-7 Metric labels are bounded.** No user-controlled free text becomes a label;
model names are allowed, prompts and ids from bodies are not.

**PRIV-8 Fixtures are synthetic.** Committed fixtures hold no real keys, emails or prompts.
