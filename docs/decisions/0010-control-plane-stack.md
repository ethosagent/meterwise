# 0010. Control plane on the stdlib mux, with OTel API and OTLP/HTTP

- **Status:** Accepted
- **Date:** 2026-11-09
- **Source:** Plan 01 sections 3 and 6; ADR 0005, ADR 0007

## Context

Plan 01 allows `chi` or the Go 1.22 mux patterns, and asks for OpenTelemetry behind an env var. Options for routing: `chi` (extra dependency, already on the H6 list) or `net/http.ServeMux` method patterns (chosen). Options for tracing: `otelhttp` contrib middleware (a module outside the H6 prefix) or a 30-line middleware on the OTel API (chosen). Options for export: gRPC or OTLP/HTTP (chosen: fewer transitive modules, works through plain proxies).

## Decision

- Routes are `ServeMux` patterns registered through one helper that requires a row in `contract.Routes`; `api.New` fails when handlers and table disagree. `chi` stays on the allowlist unused.
- Telemetry: the `go.opentelemetry.io/otel` API in `internal/api`, the SDK and `otlptracehttp` exporter in `cmd/meterwised` only. `OTEL_EXPORTER_OTLP_ENDPOINT` is a base URL; `/v1/traces` is appended. Unset means a no-op provider.
- `goose` runs through `pgx/v5/stdlib` inside `internal/store` (`store.Migrate`, `store.Status`); `cmd` never imports goose. Migrations are embedded from `internal/store/migrations`.
- The principal seam is `api.PrincipalResolver` (`Resolve(*http.Request)`), distinct from `contract.PrincipalResolver` (identity to principal), which Plan 02 keeps.

## Consequences

No new H6 prefixes: pgx and goose stay in `store`, otel stays in `api` and `cmd`. Trace spans are per request on the control plane only; the data plane (tollgate) has no OTel hook in v0.1.0. Enforced by: archtest H6, `TestNewRequiresDependenciesAndMatchesTheRouteTable`, `TestPrincipalReachesTheHandlerOnlyThroughTheResolver`, `TestServeBootsAllThreeListenersAndStreamsThroughTollgate` (collector receives `/v1/traces`).
