package metrics

import (
	"log"
	"sync"
)

var (
	globalStore *Store
	initOnce    sync.Once
	initErr     error
)

// Init initializes the global metrics store.
// This should be called once at application startup.
// It is safe to call multiple times; subsequent calls are no-ops.
func Init() error {
	initOnce.Do(func() {
		globalStore, initErr = NewStore()
		if initErr != nil {
			log.Printf("metrics: failed to initialize store: %v", initErr)
		}
	})
	return initErr
}

// RecordInvocation increments the invocation count for the given mode.
// If the store is not initialized, this is a no-op (logs a warning).
func RecordInvocation(mode Mode) {
	if globalStore == nil {
		// Attempt lazy initialization
		if err := Init(); err != nil {
			log.Printf("metrics: cannot record invocation, store not initialized: %v", err)
			return
		}
	}

	if err := globalStore.Increment(mode); err != nil {
		log.Printf("metrics: failed to record invocation for %s: %v", mode, err)
	}
}

// GetStats returns the cumulative invocation counts for all modes.
// Returns nil if the store is not initialized.
func GetStats() map[Mode]int64 {
	if globalStore == nil {
		return nil
	}

	stats, err := globalStore.GetAllTotals()
	if err != nil {
		log.Printf("metrics: failed to get stats: %v", err)
		return nil
	}

	return stats
}

// GetTotalForMode returns the cumulative count for a specific mode.
// Returns 0 if the store is not initialized or on error.
func GetTotalForMode(mode Mode) int64 {
	if globalStore == nil {
		return 0
	}

	total, err := globalStore.GetTotalByMode(mode)
	if err != nil {
		log.Printf("metrics: failed to get total for %s: %v", mode, err)
		return 0
	}

	return total
}

// Close closes the global metrics store.
// Should be called at application shutdown.
func Close() error {
	if globalStore != nil {
		return globalStore.Close()
	}
	return nil
}

// GetStore returns the global store instance.
// This is primarily for testing purposes.
func GetStore() *Store {
	return globalStore
}

// SetStoreForTesting sets the global store instance for testing purposes.
// This should only be used in tests.
func SetStoreForTesting(store *Store) {
	globalStore = store
}

// ResetForTesting resets the global state for testing purposes.
// This should only be used in tests.
func ResetForTesting() {
	if globalStore != nil {
		_ = globalStore.Close()
	}
	globalStore = nil
	initOnce = sync.Once{}
	initErr = nil
}
