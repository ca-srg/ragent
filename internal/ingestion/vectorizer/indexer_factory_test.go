package vectorizer

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServiceFactory_CreateVectorStore_SQLite(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &Config{
		VectorDBBackend: "sqlite",
		SqliteVecDBPath: dbPath,
	}

	sf := NewServiceFactory(cfg)
	store, err := sf.CreateVectorStore()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil VectorStore")
	}
}

func TestServiceFactory_CreateVectorStore_UnknownBackend(t *testing.T) {
	cfg := &Config{
		VectorDBBackend: "unknown",
	}

	sf := NewServiceFactory(cfg)
	store, err := sf.CreateVectorStore()

	if err == nil {
		t.Fatal("expected error for unknown backend, got nil")
	}
	if store != nil {
		t.Fatalf("expected nil store, got %T", store)
	}
	if !strings.Contains(err.Error(), "unsupported VECTOR_DB_BACKEND") {
		t.Errorf("expected error to contain %q, got: %v", "unsupported VECTOR_DB_BACKEND", err)
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("expected error to contain %q, got: %v", "unknown", err)
	}
}

func TestServiceFactory_CreateVectorStore_EmptyBackend(t *testing.T) {
	cfg := &Config{
		VectorDBBackend: "",
	}

	sf := NewServiceFactory(cfg)
	store, err := sf.CreateVectorStore()

	if err == nil {
		t.Fatal("expected error for empty backend, got nil")
	}
	if store != nil {
		t.Fatalf("expected nil store, got %T", store)
	}
	if !strings.Contains(err.Error(), "unsupported VECTOR_DB_BACKEND") {
		t.Errorf("expected error to contain %q, got: %v", "unsupported VECTOR_DB_BACKEND", err)
	}
}

func TestServiceFactory_CreateVectorStore_S3_DispatchesToS3(t *testing.T) {
	// S3 backend with an empty bucket name triggers NewS3VectorService's own
	// validation, confirming that dispatch reached the s3 path.
	cfg := &Config{
		VectorDBBackend:   "s3",
		AWSS3VectorBucket: "",
		AWSS3VectorIndex:  "idx",
		S3VectorRegion:    "us-east-1",
		RetryAttempts:     3,
		RetryDelay:        time.Second,
	}

	sf := NewServiceFactory(cfg)
	store, err := sf.CreateVectorStore()

	if err == nil {
		t.Fatal("expected error from s3 validation, got nil")
	}
	if store != nil {
		t.Fatalf("expected nil store, got %T", store)
	}
	if strings.Contains(err.Error(), "unsupported VECTOR_DB_BACKEND") {
		t.Errorf("dispatch should have reached s3 path, but got backend error: %v", err)
	}
}
