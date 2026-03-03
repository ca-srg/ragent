package observability

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	otlpmetricgrpc "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	otlpmetrichttp "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// InitMeter builds a meter provider for exporting application metrics.
func InitMeter(ctx context.Context, cfg *Config) (*sdkmetric.MeterProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("observability: meter initialization requires a config")
	}

	if !cfg.Enabled {
		mp := sdkmetric.NewMeterProvider()
		otel.SetMeterProvider(mp)
		return mp, nil
	}

	exporter, err := NewOTLPMetricExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("observability: failed to create OTLP metric exporter: %w", err)
	}

	mp, err := NewMeterProvider(ctx, cfg, exporter)
	if err != nil {
		return nil, err
	}

	otel.SetMeterProvider(mp)
	return mp, nil
}

// NewMeterProvider constructs a MeterProvider using the supplied exporter and configuration.
func NewMeterProvider(ctx context.Context, cfg *Config, exporter sdkmetric.Exporter) (*sdkmetric.MeterProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("observability: meter provider requires a config")
	}

	if !cfg.Enabled {
		return sdkmetric.NewMeterProvider(), nil
	}

	if exporter == nil {
		return nil, fmt.Errorf("observability: metric exporter cannot be nil when OpenTelemetry is enabled")
	}

	res, err := newResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("observability: failed to build resource information: %w", err)
	}

	reader := sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(cfg.MetricExportInterval))

	return sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(reader),
	), nil
}

// NewOTLPMetricExporter constructs an OTLP metric exporter based on the provided configuration.
func NewOTLPMetricExporter(ctx context.Context, cfg *Config) (sdkmetric.Exporter, error) {
	if cfg == nil {
		return nil, fmt.Errorf("observability: metric exporter requires a config")
	}

	switch cfg.ExporterProtocol {
	case defaultExporterProtocol:
		return newHTTPMetricExporter(ctx, cfg)
	case protocolGRPC:
		return newGRPCMetricExporter(ctx, cfg)
	default:
		return nil, fmt.Errorf("observability: unsupported metric exporter protocol %q", cfg.ExporterProtocol)
	}
}

func newHTTPMetricExporter(ctx context.Context, cfg *Config) (sdkmetric.Exporter, error) {
	endpoint, err := normalizeOTLPHTTPPath(cfg.ExporterEndpoint, "/v1/metrics")
	if err != nil {
		return nil, fmt.Errorf("observability: invalid OTLP HTTP endpoint: %w", err)
	}

	options := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpointURL(endpoint),
	}

	if strings.HasPrefix(endpoint, "http://") {
		options = append(options, otlpmetrichttp.WithInsecure())
	}

	return otlpmetrichttp.New(ctx, options...)
}

func newGRPCMetricExporter(ctx context.Context, cfg *Config) (sdkmetric.Exporter, error) {
	endpoint, insecure, err := parseGRPCEndpoint(cfg.ExporterEndpoint)
	if err != nil {
		return nil, fmt.Errorf("observability: invalid OTLP gRPC endpoint: %w", err)
	}

	options := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(endpoint),
	}

	if insecure {
		options = append(options, otlpmetricgrpc.WithInsecure())
	}

	return otlpmetricgrpc.New(ctx, options...)
}
