package observability

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ca-srg/ragent/internal/types"
)

const (
	defaultServiceName      = "ragent"
	defaultExporterProtocol = "http/protobuf"
	protocolGRPC            = "grpc"
	resourceServiceNameKey  = "service.name"
)

// Config keeps OpenTelemetry runtime settings resolved from the global configuration.
type Config struct {
	Enabled              bool
	ServiceName          string
	ExporterEndpoint     string
	ExporterProtocol     string
	ResourceAttributes   map[string]string
	TracesSampler        string
	TracesSamplerArg     float64
	MetricExportInterval time.Duration
}

// LoadConfig resolves observability specific configuration from the root config.
func LoadConfig(cfg *types.Config) (*Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("observability: nil root configuration provided")
	}

	resourceAttributes, err := parseResourceAttributes(cfg.OTelResourceAttributes)
	if err != nil {
		return nil, fmt.Errorf("observability: failed to parse resource attributes: %w", err)
	}

	otelCfg := &Config{
		Enabled:            cfg.OTelEnabled,
		ServiceName:        strings.TrimSpace(cfg.OTelServiceName),
		ExporterEndpoint:   strings.TrimSpace(cfg.OTelExporterOTLPEndpoint),
		ExporterProtocol:   strings.TrimSpace(cfg.OTelExporterOTLPProtocol),
		ResourceAttributes: resourceAttributes,
		TracesSampler:      strings.TrimSpace(cfg.OTelTracesSampler),
		TracesSamplerArg:   cfg.OTelTracesSamplerArg,
	}

	if err := otelCfg.Validate(); err != nil {
		return nil, err
	}

	return otelCfg, nil
}

// Validate ensures the configuration has all required properties before initialization.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("observability: config is nil")
	}

	// Normalise defaults
	if c.ServiceName == "" {
		c.ServiceName = defaultServiceName
	}

	c.ExporterProtocol = strings.ToLower(strings.TrimSpace(c.ExporterProtocol))
	if c.ExporterProtocol == "" {
		c.ExporterProtocol = defaultExporterProtocol
	}

	if c.TracesSampler == "" {
		c.TracesSampler = "always_on"
	}

	if c.MetricExportInterval <= 0 {
		c.MetricExportInterval = 60 * time.Second
	}

	if !c.Enabled {
		c.ensureResourceDefaults()
		return nil
	}

	if strings.TrimSpace(c.ExporterEndpoint) == "" {
		return fmt.Errorf("observability: OTLP exporter endpoint is required when OpenTelemetry is enabled")
	}

	switch c.ExporterProtocol {
	case defaultExporterProtocol:
		if !strings.HasPrefix(c.ExporterEndpoint, "http://") && !strings.HasPrefix(c.ExporterEndpoint, "https://") {
			return fmt.Errorf("observability: OTLP exporter endpoint must include http or https scheme when using http/protobuf protocol")
		}

		parsed, err := url.Parse(c.ExporterEndpoint)
		if err != nil {
			return fmt.Errorf("observability: invalid OTLP exporter endpoint: %w", err)
		}
		if parsed.Host == "" {
			return fmt.Errorf("observability: OTLP exporter endpoint must include a host when using http/protobuf protocol")
		}
	case protocolGRPC:
		if strings.Contains(c.ExporterEndpoint, "://") {
			parsed, err := url.Parse(c.ExporterEndpoint)
			if err != nil {
				return fmt.Errorf("observability: invalid OTLP exporter endpoint for grpc protocol: %w", err)
			}
			if parsed.Host == "" {
				return fmt.Errorf("observability: OTLP exporter endpoint must include a host when scheme is provided for grpc protocol")
			}
		} else if !strings.Contains(c.ExporterEndpoint, ":") {
			return fmt.Errorf("observability: OTLP exporter endpoint should include host:port when using grpc protocol")
		}
	default:
		return fmt.Errorf("observability: unsupported OTLP exporter protocol %q", c.ExporterProtocol)
	}

	if c.TracesSamplerArg < 0 {
		return fmt.Errorf("observability: traces sampler argument must be non-negative")
	}

	if strings.EqualFold(c.TracesSampler, "traceidratio") {
		if c.TracesSamplerArg <= 0 || c.TracesSamplerArg > 1 {
			return fmt.Errorf("observability: traces sampler argument must be between 0 and 1 when sampler is traceidratio")
		}
	}

	c.ensureResourceDefaults()

	return nil
}

func parseResourceAttributes(input string) (map[string]string, error) {
	attributes := make(map[string]string)

	if strings.TrimSpace(input) == "" {
		return attributes, nil
	}

	pairs := strings.Split(input, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		keyValue := strings.SplitN(pair, "=", 2)
		if len(keyValue) != 2 {
			return nil, fmt.Errorf("invalid resource attribute %q", pair)
		}

		key := strings.TrimSpace(keyValue[0])
		value := strings.TrimSpace(keyValue[1])
		if key == "" {
			return nil, fmt.Errorf("resource attribute key cannot be empty")
		}

		attributes[key] = value
	}

	return attributes, nil
}

func (c *Config) ensureResourceDefaults() {
	if c.ResourceAttributes == nil {
		c.ResourceAttributes = make(map[string]string)
	}

	// Ensure service.name is always present to meet OTel semantic conventions.
	if _, ok := c.ResourceAttributes[resourceServiceNameKey]; !ok && c.ServiceName != "" {
		c.ResourceAttributes[resourceServiceNameKey] = c.ServiceName
	}
}

// Init initializes OpenTelemetry tracing and metrics based on the root configuration.
func Init(rootCfg *types.Config) (ShutdownFunc, error) {
	defaultShutdown := func(context.Context) error { return nil }

	otelCfg, err := LoadConfig(rootCfg)
	if err != nil {
		return defaultShutdown, err
	}

	ctx := context.Background()

	tracerProvider, err := InitTracer(ctx, otelCfg)
	if err != nil {
		return defaultShutdown, err
	}

	meterProvider, err := InitMeter(ctx, otelCfg)
	if err != nil {
		shutdown := NewShutdownFunc(tracerProvider, nil)
		_ = shutdown(ctx) // Best-effort cleanup before returning error.
		return defaultShutdown, err
	}

	return NewShutdownFunc(tracerProvider, meterProvider), nil
}
