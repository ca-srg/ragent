package slackbot

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Metrics struct {
	Requests       atomic.Int64
	Responses      atomic.Int64
	Errors         atomic.Int64
	TotalLatencyNs atomic.Int64
}

func (m *Metrics) RecordRequest() { m.Requests.Add(1) }
func (m *Metrics) RecordResponse(d time.Duration) {
	m.Responses.Add(1)
	m.TotalLatencyNs.Add(d.Nanoseconds())
}
func (m *Metrics) RecordError() { m.Errors.Add(1) }

var (
	slackMetricsOnce      sync.Once
	slackRequestCounter   metric.Int64Counter
	slackErrorCounter     metric.Int64Counter
	slackLatencyHistogram metric.Float64Histogram
)

func initSlackOTelMetrics() {
	slackMetricsOnce.Do(func() {
		meter := otel.Meter("ragent/slackbot")

		var err error
		slackRequestCounter, err = meter.Int64Counter(
			"ragent.slack.requests.total",
			metric.WithDescription("Total Slack bot requests handled"),
		)
		if err != nil {
			log.Printf("observability: failed to create slack request counter: %v", err)
		}

		slackErrorCounter, err = meter.Int64Counter(
			"ragent.slack.errors.total",
			metric.WithDescription("Total Slack bot errors"),
		)
		if err != nil {
			log.Printf("observability: failed to create slack error counter: %v", err)
		}

		slackLatencyHistogram, err = meter.Float64Histogram(
			"ragent.slack.response_time",
			metric.WithDescription("Slack bot response time (ms)"),
			metric.WithUnit("ms"),
		)
		if err != nil {
			log.Printf("observability: failed to create slack latency histogram: %v", err)
		}
	})
}

func recordSlackMetrics(ctx context.Context, attrs []attribute.KeyValue, duration time.Duration, hadError bool) {
	initSlackOTelMetrics()
	if slackRequestCounter != nil {
		slackRequestCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if slackLatencyHistogram != nil {
		slackLatencyHistogram.Record(ctx, float64(duration.Milliseconds()), metric.WithAttributes(attrs...))
	}
	if hadError && slackErrorCounter != nil {
		slackErrorCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}
