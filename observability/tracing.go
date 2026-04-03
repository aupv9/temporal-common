package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// TracingConfig holds OTLP exporter configuration.
type TracingConfig struct {
	// OTLPEndpoint is the gRPC endpoint of the OTLP collector (e.g. "localhost:4317").
	// If empty, tracing is disabled and SetupTracing returns nil, nil.
	OTLPEndpoint string

	// ServiceName is the OTel resource service.name attribute.
	ServiceName string

	// ServiceVersion is the OTel resource service.version attribute.
	ServiceVersion string

	// Insecure disables TLS for the OTLP connection. Set true for local dev.
	Insecure bool
}

// SetupTracing initialises an OTLP trace exporter and registers it as the
// global OpenTelemetry TracerProvider. The Temporal SDK's tracing interceptor
// (if added via Config.Interceptors) will pick this up automatically.
//
// Returns the TracerProvider and a shutdown function. Call shutdown on process exit
// to flush pending spans.
//
// Returns nil, nil, nil when cfg.OTLPEndpoint is empty (tracing disabled).
func SetupTracing(cfg TracingConfig) (oteltrace.TracerProvider, func(context.Context) error, error) {
	if cfg.OTLPEndpoint == "" {
		return nil, func(_ context.Context) error { return nil }, nil
	}

	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(context.Background(), opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create OTel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	// Register as global provider so any interceptor built from otel.Tracer() works.
	otel.SetTracerProvider(tp)

	return tp, tp.Shutdown, nil
}
