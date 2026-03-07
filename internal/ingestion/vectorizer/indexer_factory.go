package vectorizer

import (
	"fmt"
	"log"

	"github.com/ca-srg/ragent/internal/ingestion/csv"
	"github.com/ca-srg/ragent/internal/ingestion/pdf"
	"github.com/ca-srg/ragent/internal/pkg/opensearch"
	"github.com/ca-srg/ragent/internal/pkg/s3vector"
	"github.com/ca-srg/ragent/internal/pkg/sqlitevec"
)

// Compile-time interface satisfaction checks moved here to avoid import cycles.
var _ VectorStore = (*s3vector.S3VectorService)(nil)
var _ VectorStore = (*sqlitevec.SqliteVecStore)(nil)

// IndexerFactory creates OpenSearch indexers based on configuration
type IndexerFactory struct {
	config *Config
}

// NewIndexerFactory creates a new indexer factory
func NewIndexerFactory(config *Config) *IndexerFactory {
	return &IndexerFactory{
		config: config,
	}
}

// CreateOpenSearchIndexer creates an OpenSearchIndexer based on configuration
func (f *IndexerFactory) CreateOpenSearchIndexer(indexName string, dimension int) (OpenSearchIndexer, error) {
	if f.config == nil {
		return nil, fmt.Errorf("configuration cannot be nil")
	}

	// Check if OpenSearch is configured
	if f.config.OpenSearchEndpoint == "" {
		log.Println("OpenSearch endpoint not configured, skipping OpenSearch indexer creation")
		return nil, nil
	}

	// Create OpenSearch client configuration
	openSearchConfig := &opensearch.Config{
		Endpoint:          f.config.OpenSearchEndpoint,
		Region:            f.config.OpenSearchRegion,
		InsecureSkipTLS:   f.config.OpenSearchInsecureSkipTLS,
		RateLimit:         f.config.OpenSearchRateLimit,
		RateBurst:         f.config.OpenSearchRateBurst,
		ConnectionTimeout: f.config.OpenSearchConnectionTimeout,
		RequestTimeout:    f.config.OpenSearchRequestTimeout,
		MaxRetries:        f.config.OpenSearchMaxRetries,
		RetryDelay:        f.config.OpenSearchRetryDelay,
		MaxConnections:    f.config.OpenSearchMaxConnections,
		MaxIdleConns:      f.config.OpenSearchMaxIdleConns,
		IdleConnTimeout:   f.config.OpenSearchIdleConnTimeout,
	}

	log.Printf("Creating OpenSearch client with endpoint: %s", openSearchConfig.Endpoint)

	// Create OpenSearch client
	osClient, err := opensearch.NewClient(openSearchConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenSearch client: %w", err)
	}

	// Set default dimension if not provided
	if dimension <= 0 {
		dimension = 1024 // Default for Amazon Titan Text Embedding v2
	}

	// Create OpenSearch indexer
	indexer := NewOpenSearchIndexer(osClient, indexName, dimension)

	log.Printf("Successfully created OpenSearch indexer for index '%s' with dimension %d",
		indexName, dimension)

	return indexer, nil
}

// ValidateOpenSearchConfiguration validates OpenSearch configuration
func (f *IndexerFactory) ValidateOpenSearchConfiguration() error {
	if f.config == nil {
		return fmt.Errorf("configuration cannot be nil")
	}

	// Check required OpenSearch configuration
	if f.config.OpenSearchEndpoint == "" {
		return fmt.Errorf("OpenSearch endpoint is required")
	}

	if f.config.OpenSearchRegion == "" {
		return fmt.Errorf("OpenSearch region is required")
	}

	// Validate timeout values
	if f.config.OpenSearchConnectionTimeout <= 0 {
		log.Println("Warning: Invalid OpenSearch connection timeout, using default")
	}

	if f.config.OpenSearchRequestTimeout <= 0 {
		log.Println("Warning: Invalid OpenSearch request timeout, using default")
	}

	// Validate retry configuration
	if f.config.OpenSearchMaxRetries < 0 {
		return fmt.Errorf("OpenSearch max retries cannot be negative")
	}

	if f.config.OpenSearchRetryDelay <= 0 {
		log.Println("Warning: Invalid OpenSearch retry delay, using default")
	}

	// Validate rate limiting configuration
	if f.config.OpenSearchRateLimit <= 0 {
		log.Println("Warning: Invalid OpenSearch rate limit, using default")
	}

	if f.config.OpenSearchRateBurst <= 0 {
		log.Println("Warning: Invalid OpenSearch rate burst, using default")
	}

	// Validate connection pool configuration
	if f.config.OpenSearchMaxConnections <= 0 {
		log.Println("Warning: Invalid OpenSearch max connections, using default")
	}

	if f.config.OpenSearchMaxIdleConns <= 0 {
		log.Println("Warning: Invalid OpenSearch max idle connections, using default")
	}

	if f.config.OpenSearchIdleConnTimeout <= 0 {
		log.Println("Warning: Invalid OpenSearch idle connection timeout, using default")
	}

	log.Println("OpenSearch configuration validation completed successfully")
	return nil
}

// IsOpenSearchEnabled checks if OpenSearch is enabled and properly configured
func (f *IndexerFactory) IsOpenSearchEnabled() bool {
	if f.config == nil {
		return false
	}

	return f.config.OpenSearchEndpoint != ""
}

// GetOpenSearchConfiguration returns a copy of the OpenSearch configuration
func (f *IndexerFactory) GetOpenSearchConfiguration() *opensearch.Config {
	if f.config == nil {
		return nil
	}

	return &opensearch.Config{
		Endpoint:          f.config.OpenSearchEndpoint,
		Region:            f.config.OpenSearchRegion,
		InsecureSkipTLS:   f.config.OpenSearchInsecureSkipTLS,
		RateLimit:         f.config.OpenSearchRateLimit,
		RateBurst:         f.config.OpenSearchRateBurst,
		ConnectionTimeout: f.config.OpenSearchConnectionTimeout,
		RequestTimeout:    f.config.OpenSearchRequestTimeout,
		MaxRetries:        f.config.OpenSearchMaxRetries,
		RetryDelay:        f.config.OpenSearchRetryDelay,
		MaxConnections:    f.config.OpenSearchMaxConnections,
		MaxIdleConns:      f.config.OpenSearchMaxIdleConns,
		IdleConnTimeout:   f.config.OpenSearchIdleConnTimeout,
	}
}

// ServiceFactory creates complete VectorizerService instances with all dependencies
type ServiceFactory struct {
	indexerFactory *IndexerFactory
}

// NewServiceFactory creates a new service factory
func NewServiceFactory(config *Config) *ServiceFactory {
	return &ServiceFactory{
		indexerFactory: NewIndexerFactory(config),
	}
}

// CreateServiceConfig creates a complete ServiceConfig with all dependencies
func (sf *ServiceFactory) CreateServiceConfig(
	embeddingClient EmbeddingClient,
	s3Client VectorStore,
	metadataExtractor MetadataExtractor,
	fileScanner FileScanner,
	enableOpenSearch bool,
	opensearchIndexName string,
) (*ServiceConfig, error) {

	config := &ServiceConfig{
		Config:              sf.indexerFactory.config,
		EmbeddingClient:     embeddingClient,
		S3Client:            s3Client,
		MetadataExtractor:   metadataExtractor,
		FileScanner:         fileScanner,
		EnableOpenSearch:    enableOpenSearch,
		OpenSearchIndexName: opensearchIndexName,
	}

	// Create OpenSearch indexer if enabled
	if enableOpenSearch && sf.indexerFactory.IsOpenSearchEnabled() {
		// Validate OpenSearch configuration first
		if err := sf.indexerFactory.ValidateOpenSearchConfiguration(); err != nil {
			return nil, fmt.Errorf("OpenSearch configuration validation failed: %w", err)
		}

		// Default dimension for embeddings (can be overridden)
		dimension := 1024 // Amazon Titan Text Embedding v2

		indexer, err := sf.indexerFactory.CreateOpenSearchIndexer(opensearchIndexName, dimension)
		if err != nil {
			return nil, fmt.Errorf("failed to create OpenSearch indexer: %w", err)
		}

		config.OpenSearchIndexer = indexer
		log.Printf("OpenSearch indexer created successfully for index: %s", opensearchIndexName)
	} else {
		log.Println("OpenSearch disabled or not configured")
	}

	return config, nil
}

// CreateVectorizerServiceWithDefaults creates a VectorizerService with default settings
func (sf *ServiceFactory) CreateVectorizerServiceWithDefaults(
	embeddingClient EmbeddingClient,
	s3Client VectorStore,
	metadataExtractor MetadataExtractor,
	fileScanner FileScanner,
	enableOpenSearch bool,
	opensearchIndexName string,
) (*VectorizerService, error) {
	return sf.CreateVectorizerServiceWithCSVConfig(
		embeddingClient,
		s3Client,
		metadataExtractor,
		fileScanner,
		enableOpenSearch,
		opensearchIndexName,
		nil, // No CSV config
		nil, // No PDF reader
	)
}

// CreateVectorizerServiceWithCSVConfig creates a VectorizerService with CSV configuration
func (sf *ServiceFactory) CreateVectorizerServiceWithCSVConfig(
	embeddingClient EmbeddingClient,
	s3Client VectorStore,
	metadataExtractor MetadataExtractor,
	fileScanner FileScanner,
	enableOpenSearch bool,
	opensearchIndexName string,
	csvConfig *csv.Config,
	pdfReader *pdf.Reader,
) (*VectorizerService, error) {

	serviceConfig, err := sf.CreateServiceConfig(
		embeddingClient,
		s3Client,
		metadataExtractor,
		fileScanner,
		enableOpenSearch,
		opensearchIndexName,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create service config: %w", err)
	}

	// Add CSV configuration
	serviceConfig.CSVConfig = csvConfig
	serviceConfig.PDFReader = pdfReader

	service, err := NewVectorizerService(serviceConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create vectorizer service: %w", err)
	}

	log.Printf("VectorizerService created successfully (OpenSearch enabled: %v)", enableOpenSearch)
	return service, nil
}

// CreateVectorStore creates a VectorStore based on the configured backend.
// It reads VectorDBBackend from config and dispatches to the appropriate implementation.
func (sf *ServiceFactory) CreateVectorStore() (VectorStore, error) {
	cfg := sf.indexerFactory.config
	backend := cfg.VectorDBBackend

	log.Printf("Using vector backend: %s", backend)

	switch backend {
	case "s3":
		svc, err := s3vector.NewS3VectorService(&s3vector.S3Config{
			VectorBucketName: cfg.AWSS3VectorBucket,
			IndexName:        cfg.AWSS3VectorIndex,
			Region:           cfg.S3VectorRegion,
			MaxRetries:       cfg.RetryAttempts,
			RetryDelay:       cfg.RetryDelay,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create S3 vector service: %w", err)
		}
		return svc, nil

	case "sqlite":
		store, err := sqlitevec.NewSqliteVecStore(cfg.SqliteVecDBPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create SQLite vector store: %w", err)
		}
		return store, nil

	default:
		return nil, fmt.Errorf("unsupported VECTOR_DB_BACKEND: %q (must be \"s3\" or \"sqlite\")", backend)
	}
}

// GetFactoryInfo returns information about the factory configuration
func (sf *ServiceFactory) GetFactoryInfo() map[string]interface{} {
	info := map[string]interface{}{
		"opensearch_enabled":  sf.indexerFactory.IsOpenSearchEnabled(),
		"opensearch_endpoint": sf.indexerFactory.config.OpenSearchEndpoint,
		"opensearch_region":   sf.indexerFactory.config.OpenSearchRegion,
		"concurrency":         sf.indexerFactory.config.Concurrency,
		"retry_attempts":      sf.indexerFactory.config.RetryAttempts,
		"retry_delay":         sf.indexerFactory.config.RetryDelay.String(),
	}

	if sf.indexerFactory.IsOpenSearchEnabled() {
		info["opensearch_rate_limit"] = sf.indexerFactory.config.OpenSearchRateLimit
		info["opensearch_rate_burst"] = sf.indexerFactory.config.OpenSearchRateBurst
		info["opensearch_max_retries"] = sf.indexerFactory.config.OpenSearchMaxRetries
	}

	return info
}
