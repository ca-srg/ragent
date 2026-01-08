package metrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewStoreWithPath(t *testing.T) {
	// Create temp directory for test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stats.db")

	store, err := NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("NewStoreWithPath failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Verify database file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file was not created")
	}
}

func TestIncrement(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stats.db")

	store, err := NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("NewStoreWithPath failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Test increment
	if err := store.Increment(ModeMCP); err != nil {
		t.Fatalf("Increment failed: %v", err)
	}

	// Verify count
	today := time.Now().Format("2006-01-02")
	count, err := store.GetCountByDate(ModeMCP, today)
	if err != nil {
		t.Fatalf("GetCountByDate failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	// Increment again
	if err := store.Increment(ModeMCP); err != nil {
		t.Fatalf("Second increment failed: %v", err)
	}

	count, err = store.GetCountByDate(ModeMCP, today)
	if err != nil {
		t.Fatalf("GetCountByDate failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected count 2, got %d", count)
	}
}

func TestGetTotalByMode(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stats.db")

	store, err := NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("NewStoreWithPath failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Increment multiple times for MCP
	for i := 0; i < 5; i++ {
		if err := store.Increment(ModeMCP); err != nil {
			t.Fatalf("Increment failed: %v", err)
		}
	}

	// Increment multiple times for Slack
	for i := 0; i < 3; i++ {
		if err := store.Increment(ModeSlack); err != nil {
			t.Fatalf("Increment failed: %v", err)
		}
	}

	// Verify totals
	mcpTotal, err := store.GetTotalByMode(ModeMCP)
	if err != nil {
		t.Fatalf("GetTotalByMode failed: %v", err)
	}
	if mcpTotal != 5 {
		t.Errorf("Expected MCP total 5, got %d", mcpTotal)
	}

	slackTotal, err := store.GetTotalByMode(ModeSlack)
	if err != nil {
		t.Fatalf("GetTotalByMode failed: %v", err)
	}
	if slackTotal != 3 {
		t.Errorf("Expected Slack total 3, got %d", slackTotal)
	}
}

func TestGetAllTotals(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_stats.db")

	store, err := NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("NewStoreWithPath failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Increment various modes
	_ = store.Increment(ModeMCP)
	_ = store.Increment(ModeMCP)
	_ = store.Increment(ModeSlack)
	_ = store.Increment(ModeQuery)
	_ = store.Increment(ModeQuery)
	_ = store.Increment(ModeQuery)
	_ = store.Increment(ModeChat)

	totals, err := store.GetAllTotals()
	if err != nil {
		t.Fatalf("GetAllTotals failed: %v", err)
	}

	expected := map[Mode]int64{
		ModeMCP:   2,
		ModeSlack: 1,
		ModeQuery: 3,
		ModeChat:  1,
	}

	for mode, expectedCount := range expected {
		if totals[mode] != expectedCount {
			t.Errorf("Mode %s: expected %d, got %d", mode, expectedCount, totals[mode])
		}
	}
}

func TestModeConstants(t *testing.T) {
	// Verify mode constants are as expected
	if ModeMCP != "mcp" {
		t.Errorf("ModeMCP expected 'mcp', got '%s'", ModeMCP)
	}
	if ModeSlack != "slack" {
		t.Errorf("ModeSlack expected 'slack', got '%s'", ModeSlack)
	}
	if ModeQuery != "query" {
		t.Errorf("ModeQuery expected 'query', got '%s'", ModeQuery)
	}
	if ModeChat != "chat" {
		t.Errorf("ModeChat expected 'chat', got '%s'", ModeChat)
	}
}
