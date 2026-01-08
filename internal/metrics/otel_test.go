package metrics

import (
	"context"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestOTelMetricsIntegration(t *testing.T) {
	// Reset global state
	ResetForTesting()
	ResetOTelForTesting()
	defer func() {
		ResetForTesting()
		ResetOTelForTesting()
	}()

	// Create test store
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stats.db")
	store, err := NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Set global store for testing
	SetStoreForTesting(store)

	// Add test data to SQLite
	store.Increment(ModeMCP)
	store.Increment(ModeMCP)
	store.Increment(ModeMCP)
	store.Increment(ModeSlack)
	store.Increment(ModeQuery)
	store.Increment(ModeQuery)

	// Create a manual reader for testing
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer provider.Shutdown(context.Background())

	// Initialize OTel metrics (this will register the callback)
	err = InitOTelMetrics()
	if err != nil {
		t.Fatalf("InitOTelMetrics failed: %v", err)
	}

	// Collect metrics
	var rm metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &rm)
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	// Find our metric in the collected data
	var found bool
	expectedValues := map[string]int64{
		"mcp":   3,
		"slack": 1,
		"query": 2,
		"chat":  0,
	}

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name == "ragent.invocations.total" {
				found = true

				// Check the metric data
				gauge, ok := m.Data.(metricdata.Gauge[int64])
				if !ok {
					t.Fatalf("Expected Gauge[int64], got %T", m.Data)
				}

				// Build results map
				results := make(map[string]int64)
				for _, dp := range gauge.DataPoints {
					for _, attr := range dp.Attributes.ToSlice() {
						if string(attr.Key) == "mode" {
							results[attr.Value.AsString()] = dp.Value
						}
					}
				}

				// Verify values
				for mode, expectedCount := range expectedValues {
					if results[mode] != expectedCount {
						t.Errorf("Mode %s: expected %d, got %d", mode, expectedCount, results[mode])
					}
				}
			}
		}
	}

	if !found {
		t.Error("Metric 'ragent.invocations.total' not found in collected metrics")
	}
}

func TestOTelMetricsAfterIncrement(t *testing.T) {
	// Reset global state
	ResetForTesting()
	ResetOTelForTesting()
	defer func() {
		ResetForTesting()
		ResetOTelForTesting()
	}()

	// Create test store
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stats.db")
	store, err := NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Set global store for testing
	SetStoreForTesting(store)

	// Create a manual reader for testing
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer provider.Shutdown(context.Background())

	// Initialize OTel metrics
	err = InitOTelMetrics()
	if err != nil {
		t.Fatalf("InitOTelMetrics failed: %v", err)
	}

	// First collection - should be all zeros
	var rm1 metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &rm1)
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	// Verify first collection has zeros
	verifyMetricValues(t, rm1, map[string]int64{
		"mcp":   0,
		"slack": 0,
		"query": 0,
		"chat":  0,
	})

	// Add data after OTel initialization
	store.Increment(ModeMCP)
	store.Increment(ModeMCP)
	store.Increment(ModeSlack)

	// Second collection - should reflect the increments
	var rm2 metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &rm2)
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	// Verify second collection has correct values
	verifyMetricValues(t, rm2, map[string]int64{
		"mcp":   2,
		"slack": 1,
		"query": 0,
		"chat":  0,
	})

	// Add more data
	store.Increment(ModeQuery)
	store.Increment(ModeQuery)
	store.Increment(ModeQuery)
	store.Increment(ModeChat)

	// Third collection
	var rm3 metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &rm3)
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	// Verify cumulative totals
	verifyMetricValues(t, rm3, map[string]int64{
		"mcp":   2,
		"slack": 1,
		"query": 3,
		"chat":  1,
	})
}

func TestOTelMetricDescription(t *testing.T) {
	// Reset global state
	ResetForTesting()
	ResetOTelForTesting()
	defer func() {
		ResetForTesting()
		ResetOTelForTesting()
	}()

	// Create test store
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stats.db")
	store, err := NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()
	SetStoreForTesting(store)

	// Create a manual reader for testing
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer provider.Shutdown(context.Background())

	// Initialize OTel metrics
	err = InitOTelMetrics()
	if err != nil {
		t.Fatalf("InitOTelMetrics failed: %v", err)
	}

	// Collect metrics
	var rm metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &rm)
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	// Verify metric metadata
	for _, scopeMetrics := range rm.ScopeMetrics {
		if scopeMetrics.Scope.Name != "ragent/metrics" {
			continue
		}

		for _, m := range scopeMetrics.Metrics {
			if m.Name == "ragent.invocations.total" {
				if m.Description != "Cumulative total invocations by mode (mcp, slack, query, chat)" {
					t.Errorf("Unexpected description: %s", m.Description)
				}
				if m.Unit != "{invocations}" {
					t.Errorf("Unexpected unit: %s", m.Unit)
				}
				return
			}
		}
	}

	t.Error("Metric 'ragent.invocations.total' not found")
}

func TestOTelMetricsWithoutStore(t *testing.T) {
	// Reset global state - no store initialized
	ResetForTesting()
	ResetOTelForTesting()
	defer func() {
		ResetForTesting()
		ResetOTelForTesting()
	}()

	// Create a manual reader for testing
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer provider.Shutdown(context.Background())

	// Initialize OTel metrics without a store
	err := InitOTelMetrics()
	if err != nil {
		t.Fatalf("InitOTelMetrics failed: %v", err)
	}

	// Collect metrics
	var rm metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &rm)
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	// Should still have metrics, all with value 0
	verifyMetricValues(t, rm, map[string]int64{
		"mcp":   0,
		"slack": 0,
		"query": 0,
		"chat":  0,
	})
}

func TestOTelMetricAttributes(t *testing.T) {
	// Reset global state
	ResetForTesting()
	ResetOTelForTesting()
	defer func() {
		ResetForTesting()
		ResetOTelForTesting()
	}()

	// Create test store
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stats.db")
	store, err := NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()
	SetStoreForTesting(store)

	// Add some data
	store.Increment(ModeMCP)
	store.Increment(ModeSlack)

	// Create a manual reader for testing
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer provider.Shutdown(context.Background())

	// Initialize OTel metrics
	err = InitOTelMetrics()
	if err != nil {
		t.Fatalf("InitOTelMetrics failed: %v", err)
	}

	// Collect metrics
	var rm metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &rm)
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	// Verify that each data point has the correct "mode" attribute
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name == "ragent.invocations.total" {
				gauge, ok := m.Data.(metricdata.Gauge[int64])
				if !ok {
					t.Fatalf("Expected Gauge[int64], got %T", m.Data)
				}

				// Should have 4 data points (one per mode)
				if len(gauge.DataPoints) != 4 {
					t.Errorf("Expected 4 data points, got %d", len(gauge.DataPoints))
				}

				// Verify each data point has exactly one attribute "mode"
				for _, dp := range gauge.DataPoints {
					attrs := dp.Attributes.ToSlice()
					if len(attrs) != 1 {
						t.Errorf("Expected 1 attribute, got %d", len(attrs))
					}
					if len(attrs) > 0 && string(attrs[0].Key) != "mode" {
						t.Errorf("Expected attribute key 'mode', got '%s'", attrs[0].Key)
					}
				}
				return
			}
		}
	}

	t.Error("Metric 'ragent.invocations.total' not found")
}

// verifyMetricValues is a helper function to verify metric values
func verifyMetricValues(t *testing.T, rm metricdata.ResourceMetrics, expected map[string]int64) {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name == "ragent.invocations.total" {
				gauge, ok := m.Data.(metricdata.Gauge[int64])
				if !ok {
					t.Fatalf("Expected Gauge[int64], got %T", m.Data)
				}

				results := make(map[string]int64)
				for _, dp := range gauge.DataPoints {
					for _, attr := range dp.Attributes.ToSlice() {
						if string(attr.Key) == "mode" {
							results[attr.Value.AsString()] = dp.Value
						}
					}
				}

				for mode, expectedCount := range expected {
					if results[mode] != expectedCount {
						t.Errorf("Mode %s: expected %d, got %d", mode, expectedCount, results[mode])
					}
				}
				return
			}
		}
	}

	t.Error("Metric 'ragent.invocations.total' not found")
}
