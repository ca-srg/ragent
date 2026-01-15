package mcpserver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/metrics"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHandleSDKToolCall_RecordsInvocationCount(t *testing.T) {
	// 1. Reset global metrics state for clean test
	metrics.ResetForTesting()
	defer metrics.ResetForTesting()

	// 2. Create temporary test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stats.db")
	store, err := metrics.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// 3. Inject test store into global state
	metrics.SetStoreForTesting(store)

	// 4. Verify initial count is 0
	today := time.Now().Format("2006-01-02")
	initialCount, _ := store.GetCountByDate(metrics.ModeMCP, today)
	if initialCount != 0 {
		t.Errorf("Expected initial count 0, got %d", initialCount)
	}

	// 5. Create HybridSearchHandler with nil adapter
	// (We only test that RecordInvocation is called, not the actual search)
	handler := &HybridSearchHandler{adapter: nil}

	// 6. Call HandleSDKToolCall - it will panic due to nil adapter,
	// but RecordInvocation should be called first
	func() {
		defer func() { recover() }() // Catch expected panic
		ctx := context.Background()
		req := &mcp.CallToolRequest{
			Params: &mcp.CallToolParamsRaw{Name: "hybrid_search"},
		}
		_, _ = handler.HandleSDKToolCall(ctx, req)
	}()

	// 7. Verify count increased to 1
	count, err := store.GetCountByDate(metrics.ModeMCP, today)
	if err != nil {
		t.Fatalf("GetCountByDate failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1 after first tool call, got %d", count)
	}

	// 8. Call again
	func() {
		defer func() { recover() }()
		ctx := context.Background()
		req := &mcp.CallToolRequest{
			Params: &mcp.CallToolParamsRaw{Name: "hybrid_search"},
		}
		_, _ = handler.HandleSDKToolCall(ctx, req)
	}()

	// 9. Verify count increased to 2
	count, err = store.GetCountByDate(metrics.ModeMCP, today)
	if err != nil {
		t.Fatalf("GetCountByDate failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected count 2 after second tool call, got %d", count)
	}
}

func TestSlackSearchHandler_RecordsInvocationCount(t *testing.T) {
	// 1. Reset global metrics state
	metrics.ResetForTesting()
	defer metrics.ResetForTesting()

	// 2. Create temporary test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stats.db")
	store, err := metrics.NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// 3. Inject test store
	metrics.SetStoreForTesting(store)

	// 4. Verify initial count is 0
	today := time.Now().Format("2006-01-02")
	initialCount, _ := store.GetCountByDate(metrics.ModeMCP, today)
	if initialCount != 0 {
		t.Errorf("Expected initial count 0, got %d", initialCount)
	}

	// 5. Create SlackSearchHandler with nil adapter
	handler := &SlackSearchHandler{adapter: nil}

	// 6. Call HandleSDKToolCall
	func() {
		defer func() { recover() }()
		ctx := context.Background()
		req := &mcp.CallToolRequest{
			Params: &mcp.CallToolParamsRaw{Name: "slack_search"},
		}
		_, _ = handler.HandleSDKToolCall(ctx, req)
	}()

	// 7. Verify count increased to 1
	count, err := store.GetCountByDate(metrics.ModeMCP, today)
	if err != nil {
		t.Fatalf("GetCountByDate failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1 after tool call, got %d", count)
	}
}
