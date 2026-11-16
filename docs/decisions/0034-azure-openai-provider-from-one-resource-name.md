# 0034. Azure OpenAI provider from one resource name

- **Status:** Accepted
- **Date:** 2026-11-16
- **Source:** request to run opencode against an Azure OpenAI deployment through Meterwise

## Context

The proxy knew three providers with fixed hosts (`openai`, `anthropic`, `openrouter`). Azure OpenAI has
one host per customer resource (`<name>.openai.azure.com`) and authenticates with an `api-key` header.
Options: a per-workspace upstream URL (a schema change, plus SSRF rules for a customer-supplied host); a
process-wide resource name (one env var, no new trust boundary); or nothing (Azure users bypass Meterwise).

Azure's v1 API (`/openai/v1/chat/completions`, deployment name as `model`) is OpenAI chat-completions on
the wire, so tollgate's `WireOpenAIChat` parses it without change. tollgate forwards every request header
except hop-by-hop ones and its own, so `api-key` already reaches the upstream, and no Meterwise or
tollgate log line carries request headers.

## Decision

`AZURE_RESOURCE_NAME` (optional, validated as a bare Azure resource name) adds an `azure` provider that
maps `/w/{id}/azure/{rest}` to `https://<name>.openai.azure.com/openai/{rest}` with `WireOpenAIChat`.
Unset, `azure` stays an unknown provider (404).

## Consequences

- One Azure resource per deployment of Meterwise. Several resources, or one per workspace, need the
  per-workspace upstream option and a new ADR.
- Clients must use chat-completions. Azure's Responses API (`/openai/v1/responses`, the default of
  `@ai-sdk/azure`) passes through tollgate's wire table unparsed; use `@ai-sdk/openai-compatible`.
- The `api-key` value is the customer's key and is treated like `Authorization`: passed through, never
  stored or logged (non-negotiable 2).
- Enforced by `internal/config/azure_test.go` (name validation, shown in `config check`) and
  `internal/dataplane/azure_test.go` (host mapping, path, `api-key` reaches the upstream unchanged).
