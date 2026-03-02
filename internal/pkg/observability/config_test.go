package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/types"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

func TestInitExportsToOTLPHTTP(t *testing.T) {
	var traceRequests atomic.Int32
	var metricRequests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/traces":
			traceRequests.Add(1)
		case "/v1/metrics":
			metricRequests.Add(1)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)

	cfg := &types.Config{
		OTelEnabled:              true,
		OTelServiceName:          "ragent-test",
		OTelExporterOTLPEndpoint: server.URL,
		OTelExporterOTLPProtocol: "http/protobuf",
		OTelResourceAttributes:   "service.namespace=ragent-test,environment=test",
		OTelTracesSampler:        "always_on",
		OTelTracesSamplerArg:     1.0,
	}

	shutdown, err := Init(cfg)
	require.NoError(t, err)

	ctx := context.Background()
	_, span := otel.Tracer("ragent/test").Start(ctx, "integration-span")
	span.End()

	meter := otel.Meter("ragent/test")
	counter, err := meter.Int64Counter("ragent.test.counter", metric.WithDescription("test counter"))
	require.NoError(t, err)
	counter.Add(ctx, 1)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, shutdown(shutdownCtx))

	require.GreaterOrEqual(t, traceRequests.Load(), int32(1), "no trace export received")
	require.GreaterOrEqual(t, metricRequests.Load(), int32(1), "no metric export received")
}
