package mcpserver

import (
	"context"
	"log"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	mcpMetricsOnce      sync.Once
	mcpRequestCounter   metric.Int64Counter
	mcpErrorCounter     metric.Int64Counter
	mcpLatencyHistogram metric.Float64Histogram
)

func initMCPMetrics() {
	mcpMetricsOnce.Do(func() {
		meter := otel.Meter("ragent/mcpserver")

		var err error
		mcpRequestCounter, err = meter.Int64Counter(
			"ragent.mcp.requests.total",
			metric.WithDescription("Total MCP server tool requests"),
		)
		if err != nil {
			log.Printf("observability: failed to create MCP request counter: %v", err)
		}

		mcpErrorCounter, err = meter.Int64Counter(
			"ragent.mcp.errors.total",
			metric.WithDescription("Total MCP server tool errors"),
		)
		if err != nil {
			log.Printf("observability: failed to create MCP error counter: %v", err)
		}

		mcpLatencyHistogram, err = meter.Float64Histogram(
			"ragent.mcp.response_time",
			metric.WithDescription("MCP server tool response time (ms)"),
			metric.WithUnit("ms"),
		)
		if err != nil {
			log.Printf("observability: failed to create MCP latency histogram: %v", err)
		}
	})
}

func recordMCPMetrics(ctx context.Context, attrs []attribute.KeyValue, duration time.Duration, errType string) {
	initMCPMetrics()
	if mcpRequestCounter != nil {
		mcpRequestCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if mcpLatencyHistogram != nil {
		mcpLatencyHistogram.Record(ctx, float64(duration.Milliseconds()), metric.WithAttributes(attrs...))
	}
	if errType != "" && mcpErrorCounter != nil {
		errAttrs := make([]attribute.KeyValue, len(attrs)+1)
		copy(errAttrs, attrs)
		errAttrs[len(attrs)] = attribute.String("error.type", errType)
		mcpErrorCounter.Add(ctx, 1, metric.WithAttributes(errAttrs...))
	}
}
