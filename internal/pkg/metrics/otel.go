package metrics

import (
	"context"
	"log"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	otelMetricsOnce       sync.Once
	otelRegistrationError error
)

// InitOTelMetrics initializes OpenTelemetry metrics for invocation counts.
// It registers an observable gauge that reports cumulative totals from SQLite.
// This should be called after observability.Init() has been called.
func InitOTelMetrics() error {
	otelMetricsOnce.Do(func() {
		meter := otel.Meter("ragent/metrics")

		_, err := meter.Int64ObservableGauge(
			"ragent.invocations.total",
			metric.WithDescription("Cumulative total invocations by mode (mcp, slack, query, chat)"),
			metric.WithUnit("{invocations}"),
			metric.WithInt64Callback(invocationCallback),
		)
		if err != nil {
			log.Printf("metrics: failed to create invocation gauge: %v", err)
			otelRegistrationError = err
			return
		}
	})
	return otelRegistrationError
}

// invocationCallback is called by the OTel SDK to collect current metric values.
// It reads cumulative totals from SQLite and reports them as gauge values.
func invocationCallback(_ context.Context, observer metric.Int64Observer) error {
	stats := GetStats()
	if stats == nil {
		// Store not initialized, report zeros
		for _, mode := range []Mode{ModeMCP, ModeSlack, ModeQuery, ModeChat} {
			observer.Observe(0, metric.WithAttributes(
				attribute.String("mode", string(mode)),
			))
		}
		return nil
	}

	for mode, count := range stats {
		observer.Observe(count, metric.WithAttributes(
			attribute.String("mode", string(mode)),
		))
	}

	return nil
}

// ResetOTelForTesting resets the OTel initialization state for testing purposes.
// This should only be used in tests.
func ResetOTelForTesting() {
	otelMetricsOnce = sync.Once{}
	otelRegistrationError = nil
}
