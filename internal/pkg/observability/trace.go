package observability

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otlptracegrpc "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otlptracehttp "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// InitTracer builds a tracer provider ready for application instrumentation.
func InitTracer(ctx context.Context, cfg *Config) (*sdktrace.TracerProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("observability: tracer initialization requires a config")
	}

	if !cfg.Enabled {
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sdktrace.NeverSample()),
		)
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(defaultPropagator())
		return tp, nil
	}

	exporter, err := NewOTLPTraceExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("observability: failed to create OTLP trace exporter: %w", err)
	}

	tp, err := NewTracerProvider(ctx, cfg, exporter)
	if err != nil {
		return nil, err
	}

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(defaultPropagator())

	return tp, nil
}

// NewTracerProvider constructs a TracerProvider using the supplied exporter and configuration.
func NewTracerProvider(ctx context.Context, cfg *Config, exporter sdktrace.SpanExporter) (*sdktrace.TracerProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("observability: tracer provider requires a config")
	}

	if !cfg.Enabled {
		return sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sdktrace.NeverSample()),
		), nil
	}

	if exporter == nil {
		return nil, fmt.Errorf("observability: trace exporter cannot be nil when OpenTelemetry is enabled")
	}

	res, err := newResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("observability: failed to build resource information: %w", err)
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithSampler(samplerFromConfig(cfg)),
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exporter),
	), nil
}

// NewOTLPTraceExporter constructs an OTLP trace exporter based on the provided configuration.
func NewOTLPTraceExporter(ctx context.Context, cfg *Config) (sdktrace.SpanExporter, error) {
	if cfg == nil {
		return nil, fmt.Errorf("observability: trace exporter requires a config")
	}

	switch cfg.ExporterProtocol {
	case defaultExporterProtocol:
		return newHTTPTraceExporter(ctx, cfg)
	case protocolGRPC:
		return newGRPCTraceExporter(ctx, cfg)
	default:
		return nil, fmt.Errorf("observability: unsupported trace exporter protocol %q", cfg.ExporterProtocol)
	}
}

func newHTTPTraceExporter(ctx context.Context, cfg *Config) (sdktrace.SpanExporter, error) {
	endpoint, err := normalizeOTLPHTTPPath(cfg.ExporterEndpoint, "/v1/traces")
	if err != nil {
		return nil, fmt.Errorf("observability: invalid OTLP HTTP endpoint: %w", err)
	}

	options := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(endpoint),
	}

	if strings.HasPrefix(endpoint, "http://") {
		options = append(options, otlptracehttp.WithInsecure())
	}

	return otlptracehttp.New(ctx, options...)
}

func newGRPCTraceExporter(ctx context.Context, cfg *Config) (sdktrace.SpanExporter, error) {
	endpoint, insecure, err := parseGRPCEndpoint(cfg.ExporterEndpoint)
	if err != nil {
		return nil, fmt.Errorf("observability: invalid OTLP gRPC endpoint: %w", err)
	}

	options := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}

	if insecure {
		options = append(options, otlptracegrpc.WithInsecure())
	}

	return otlptracegrpc.New(ctx, options...)
}

func parseGRPCEndpoint(raw string) (string, bool, error) {
	endpoint := strings.TrimSpace(raw)
	if endpoint == "" {
		return "", false, fmt.Errorf("endpoint cannot be empty")
	}

	insecure := false

	if strings.Contains(endpoint, "://") {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return "", false, err
		}
		if parsed.Host == "" {
			return "", false, fmt.Errorf("endpoint must include host")
		}
		switch parsed.Scheme {
		case "http", "grpc":
			insecure = true
		case "https", "grpcs":
			insecure = false
		default:
			return "", false, fmt.Errorf("unsupported scheme %q", parsed.Scheme)
		}
		return parsed.Host, insecure, nil
	}

	// Without scheme treat connection as insecure and expect host:port.
	insecure = true
	return endpoint, insecure, nil
}

func samplerFromConfig(cfg *Config) sdktrace.Sampler {
	switch strings.ToLower(strings.TrimSpace(cfg.TracesSampler)) {
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.TracesSamplerArg))
	case "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	default:
		return sdktrace.AlwaysSample()
	}
}

func newResource(ctx context.Context, cfg *Config) (*resource.Resource, error) {
	attributes := []attribute.KeyValue{
		attribute.String(resourceServiceNameKey, cfg.ServiceName),
	}

	for key, value := range cfg.ResourceAttributes {
		if strings.EqualFold(key, resourceServiceNameKey) {
			continue
		}
		attributes = append(attributes, attribute.String(key, value))
	}

	res, err := resource.New(
		ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithHost(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(attributes...),
	)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func defaultPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}
