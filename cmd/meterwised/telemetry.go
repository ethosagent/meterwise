package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/ethosagent/meterwise/internal/config"
)

const (
	serviceName      = "meterwised"
	traceBatchWindow = time.Second
	traceFlushLimit  = 5 * time.Second
)

// Fixed telemetry failures: the exporter and url errors echo the endpoint URL,
// which may carry credentials, so callers only ever see these.
var (
	errInvalidEndpoint = errors.New("the OTLP endpoint is not a valid URL")
	errExporter        = errors.New("the OTLP exporter could not be created")
)

// newExporter follows the OTLP convention: the endpoint is a base URL and the
// signal path /v1/traces is appended (WithEndpointURL would treat it as the full URL).
func newExporter(ctx context.Context, endpoint string) (*otlptrace.Exporter, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, errInvalidEndpoint
	}
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(u.Host),
		otlptracehttp.WithURLPath(strings.TrimRight(u.Path, "/") + "/v1/traces"),
	}
	if u.Scheme == "http" {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, errExporter
	}
	return exp, nil
}

// startTelemetry returns the tracer provider and a shutdown that flushes it.
// With OTEL_EXPORTER_OTLP_ENDPOINT unset the provider is a no-op and nothing
// leaves the process; set, spans go over OTLP/HTTP to <endpoint>/v1/traces.
func startTelemetry(ctx context.Context, cfg config.Config) (trace.TracerProvider, func(context.Context) error, error) {
	if cfg.OTLPEndpoint == "" {
		return noop.NewTracerProvider(), func(context.Context) error { return nil }, nil
	}
	exp, err := newExporter(ctx, cfg.OTLPEndpoint)
	if err != nil {
		return nil, nil, fmt.Errorf("telemetry: %w", err)
	}
	res := sdkresource.NewSchemaless(
		attribute.String("service.name", serviceName),
		attribute.String("service.version", version),
		attribute.String("deployment.environment.name", cfg.Env),
	)
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(traceBatchWindow)), sdktrace.WithResource(res))
	shutdown := func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, traceFlushLimit)
		defer cancel()
		return tp.Shutdown(ctx)
	}
	return tp, shutdown, nil
}
