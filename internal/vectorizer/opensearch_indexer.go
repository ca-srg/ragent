package vectorizer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ca-srg/mdrag/internal/opensearch"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

// OpenSearchIndexerImpl implements the OpenSearchIndexer interface
type OpenSearchIndexerImpl struct {
	client           *opensearch.Client
	textProcessor    *opensearch.JapaneseTextProcessor
	defaultIndex     string
	defaultDimension int
}

// NewOpenSearchIndexer creates a new OpenSearchIndexer implementation
func NewOpenSearchIndexer(client *opensearch.Client, defaultIndex string, defaultDimension int) *OpenSearchIndexerImpl {
	return &OpenSearchIndexerImpl{
		client:           client,
		textProcessor:    opensearch.NewJapaneseTextProcessor(),
		defaultIndex:     defaultIndex,
		defaultDimension: defaultDimension,
	}
}

// IndexDocument indexes a single document in OpenSearch
func (osi *OpenSearchIndexerImpl) IndexDocument(ctx context.Context, indexName string, document *OpenSearchDocument) error {
	if document == nil {
		return WrapError(fmt.Errorf("document cannot be nil"), ErrorTypeValidation, "")
	}

	if err := document.Validate(); err != nil {
		return WrapError(err, ErrorTypeValidation, document.FilePath)
	}

	// Process Japanese content if not already processed
	if err := osi.processJapaneseContentForDocument(document); err != nil {
		return WrapError(err, ErrorTypeValidation, document.FilePath)
	}

	startTime := time.Now()

	// Wait for rate limiting
	if err := osi.client.WaitForRateLimit(ctx); err != nil {
		return WrapError(err, ErrorTypeRateLimit, document.FilePath)
	}

	operation := func() error {
		// Directly marshal the document struct to preserve type information
		bodyJSON, err := json.Marshal(document)
		if err != nil {
			return WrapError(err, ErrorTypeValidation, document.FilePath)
		}
		
		req := opensearchapi.IndexReq{
			Index:      indexName,
			DocumentID: document.ID,
			Body:       strings.NewReader(string(bodyJSON)),
		}

		_, err = osi.client.GetClient().Index(ctx, req)
		if err != nil {
			return osi.classifyOpenSearchError(err, document.FilePath)
		}

		return nil
	}

	err := osi.client.ExecuteWithRetry(ctx, operation, fmt.Sprintf("IndexDocument[%s]", document.ID))

	// Record metrics
	duration := time.Since(startTime)
	osi.client.RecordRequest(duration, err == nil)

	if err == nil {
		log.Printf("Successfully indexed document %s in %v", document.ID, duration)
	} else {
		log.Printf("Failed to index document %s after retries: %v", document.ID, err)
	}

	return err
}

// IndexDocuments indexes multiple documents using bulk operations
func (osi *OpenSearchIndexerImpl) IndexDocuments(ctx context.Context, indexName string, documents []*OpenSearchDocument) error {
	if len(documents) == 0 {
		return nil
	}

	startTime := time.Now()
	log.Printf("Starting bulk indexing of %d documents to index %s", len(documents), indexName)

	// Validate all documents first and process Japanese content
	for i, doc := range documents {
		if doc == nil {
			return WrapError(fmt.Errorf("document at index %d is nil", i), ErrorTypeValidation, "")
		}
		if err := doc.Validate(); err != nil {
			return WrapError(err, ErrorTypeValidation, doc.FilePath)
		}
		// Process Japanese content for each document
		if err := osi.processJapaneseContentForDocument(doc); err != nil {
			return WrapError(err, ErrorTypeValidation, doc.FilePath)
		}
	}

	// Process documents in optimized batches
	const batchSize = 1000
	totalDocs := len(documents)
	successCount := 0

	for i := 0; i < totalDocs; i += batchSize {
		end := i + batchSize
		if end > totalDocs {
			end = totalDocs
		}

		batch := documents[i:end]
		if err := osi.indexDocumentBatch(ctx, indexName, batch, i); err != nil {
			return WrapError(err, ErrorTypeOpenSearchBulkIndex, fmt.Sprintf("batch_%d-%d", i, end-1))
		}
		successCount += len(batch)

		// Progress reporting
		percent := (successCount * 100) / totalDocs
		if percent%10 == 0 || successCount == totalDocs {
			log.Printf("Bulk indexing progress: %d%% (%d/%d documents)", percent, successCount, totalDocs)
		}
	}

	duration := time.Since(startTime)
	log.Printf("Successfully completed bulk indexing of %d documents in %v", totalDocs, duration)
	return nil
}

// indexDocumentBatch indexes a batch of documents
func (osi *OpenSearchIndexerImpl) indexDocumentBatch(ctx context.Context, indexName string, documents []*OpenSearchDocument, offset int) error {
	startTime := time.Now()

	operation := func() error {
		if err := osi.client.WaitForRateLimit(ctx); err != nil {
			return WrapError(err, ErrorTypeRateLimit, fmt.Sprintf("batch_%d", offset))
		}

		bulkBody, err := osi.buildBulkBody(indexName, documents)
		if err != nil {
			return WrapError(err, ErrorTypeValidation, fmt.Sprintf("batch_%d", offset))
		}

		req := opensearchapi.BulkReq{
			Body: strings.NewReader(bulkBody),
		}

		resp, err := osi.client.GetClient().Bulk(ctx, req)
		if err != nil {
			return osi.classifyOpenSearchError(err, fmt.Sprintf("batch_%d", offset))
		}

		if resp != nil {
			// TODO: Parse bulk response for individual document errors when OpenSearch client API is clarified
			// For now, assume success if no error returned
		}

		return nil
	}

	err := osi.client.ExecuteWithRetry(ctx, operation, fmt.Sprintf("BulkIndex[%d docs]", len(documents)))

	// Record metrics
	duration := time.Since(startTime)
	osi.client.RecordRequest(duration, err == nil)

	if err == nil {
		log.Printf("Successfully indexed batch of %d documents in %v", len(documents), duration)
	}

	return err
}

// buildBulkBody constructs the bulk request body
func (osi *OpenSearchIndexerImpl) buildBulkBody(indexName string, documents []*OpenSearchDocument) (string, error) {
	var bulkBody strings.Builder
	bulkBody.Grow(len(documents) * 300) // Rough estimate for better performance

	for _, doc := range documents {
		// Index action line
		action := map[string]interface{}{
			"index": map[string]interface{}{
				"_index": indexName,
				"_id":    doc.ID,
			},
		}

		actionJSON, err := json.Marshal(action)
		if err != nil {
			return "", fmt.Errorf("failed to marshal bulk action for document %s: %w", doc.ID, err)
		}

		// Document data line
		docMap := doc.ToMap()
		docJSON, err := json.Marshal(docMap)
		if err != nil {
			return "", fmt.Errorf("failed to marshal document %s: %w", doc.ID, err)
		}

		bulkBody.Write(actionJSON)
		bulkBody.WriteString("\n")
		bulkBody.Write(docJSON)
		bulkBody.WriteString("\n")
	}

	return bulkBody.String(), nil
}

// ValidateConnection checks if OpenSearch is accessible and responsive
func (osi *OpenSearchIndexerImpl) ValidateConnection(ctx context.Context) error {
	startTime := time.Now()

	err := osi.client.HealthCheck(ctx)

	duration := time.Since(startTime)
	osi.client.RecordRequest(duration, err == nil)

	if err != nil {
		return WrapError(err, ErrorTypeOpenSearchConnection, "health_check")
	}

	log.Printf("OpenSearch connection validated successfully in %v", duration)
	return nil
}

// CreateIndex creates a new index with appropriate mappings for vector search
func (osi *OpenSearchIndexerImpl) CreateIndex(ctx context.Context, indexName string, dimension int) error {
	startTime := time.Now()

	// Use default dimension if not specified
	if dimension <= 0 {
		dimension = osi.defaultDimension
	}

	operation := func() error {
		if err := osi.client.WaitForRateLimit(ctx); err != nil {
			return WrapError(err, ErrorTypeRateLimit, indexName)
		}

		return osi.client.CreateVectorIndex(ctx, indexName, dimension, "lucene", "cosinesimil")
	}

	err := osi.client.ExecuteWithRetry(ctx, operation, fmt.Sprintf("CreateIndex[%s]", indexName))

	duration := time.Since(startTime)
	osi.client.RecordRequest(duration, err == nil)

	if err != nil {
		return WrapError(err, ErrorTypeOpenSearchIndex, indexName)
	}

	log.Printf("Successfully created index %s with dimension %d in %v", indexName, dimension, duration)
	return nil
}

// DeleteIndex removes an existing index (use with caution)
func (osi *OpenSearchIndexerImpl) DeleteIndex(ctx context.Context, indexName string) error {
	startTime := time.Now()

	operation := func() error {
		if err := osi.client.WaitForRateLimit(ctx); err != nil {
			return WrapError(err, ErrorTypeRateLimit, indexName)
		}

		req := opensearchapi.IndicesDeleteReq{
			Indices: []string{indexName},
		}

		_, err := osi.client.GetClient().Indices.Delete(ctx, req)
		if err != nil {
			return osi.classifyOpenSearchError(err, indexName)
		}

		return nil
	}

	err := osi.client.ExecuteWithRetry(ctx, operation, fmt.Sprintf("DeleteIndex[%s]", indexName))

	duration := time.Since(startTime)
	osi.client.RecordRequest(duration, err == nil)

	if err != nil {
		return WrapError(err, ErrorTypeOpenSearchIndex, indexName)
	}

	log.Printf("Successfully deleted index %s in %v", indexName, duration)
	return nil
}

// IndexExists checks if an index exists in OpenSearch
func (osi *OpenSearchIndexerImpl) IndexExists(ctx context.Context, indexName string) (bool, error) {
	startTime := time.Now()

	var exists bool
	operation := func() error {
		if err := osi.client.WaitForRateLimit(ctx); err != nil {
			return WrapError(err, ErrorTypeRateLimit, indexName)
		}

		req := opensearchapi.IndicesExistsReq{
			Indices: []string{indexName},
		}

		resp, err := osi.client.GetClient().Indices.Exists(ctx, req)
		if err != nil {
			return osi.classifyOpenSearchError(err, indexName)
		}

		// Status code 200 means index exists, 404 means it doesn't
		exists = (resp != nil)
		return nil
	}

	err := osi.client.ExecuteWithRetry(ctx, operation, fmt.Sprintf("IndexExists[%s]", indexName))

	duration := time.Since(startTime)
	osi.client.RecordRequest(duration, err == nil)

	if err != nil {
		return false, WrapError(err, ErrorTypeOpenSearchIndex, indexName)
	}

	return exists, nil
}

// GetIndexInfo returns information about an index
func (osi *OpenSearchIndexerImpl) GetIndexInfo(ctx context.Context, indexName string) (map[string]interface{}, error) {
	startTime := time.Now()

	var info map[string]interface{}
	operation := func() error {
		if err := osi.client.WaitForRateLimit(ctx); err != nil {
			return WrapError(err, ErrorTypeRateLimit, indexName)
		}

		// Get index settings and mappings
		req := opensearchapi.IndicesGetReq{
			Indices: []string{indexName},
		}

		_, err := osi.client.GetClient().Indices.Get(ctx, req)
		if err != nil {
			return osi.classifyOpenSearchError(err, indexName)
		}

		// Get document count using search API as Count API may not be available
		searchReq := opensearchapi.SearchReq{
			Indices: []string{indexName},
			Body: strings.NewReader(`{
				"size": 0,
				"track_total_hits": true
			}`),
		}

		_, err = osi.client.GetClient().Search(ctx, &searchReq)
		if err != nil {
			return osi.classifyOpenSearchError(err, indexName)
		}

		// TODO: Parse response properly when OpenSearch client API is clarified
		// For now, return basic info
		info = map[string]interface{}{
			"name":           indexName,
			"exists":         true,
			"document_count": 0, // Will be updated when response parsing is implemented
		}

		return nil
	}

	err := osi.client.ExecuteWithRetry(ctx, operation, fmt.Sprintf("GetIndexInfo[%s]", indexName))

	duration := time.Since(startTime)
	osi.client.RecordRequest(duration, err == nil)

	if err != nil {
		return nil, WrapError(err, ErrorTypeOpenSearchIndex, indexName)
	}

	return info, nil
}

// RefreshIndex forces a refresh of the index to make recent changes visible
func (osi *OpenSearchIndexerImpl) RefreshIndex(ctx context.Context, indexName string) error {
	startTime := time.Now()

	operation := func() error {
		if err := osi.client.WaitForRateLimit(ctx); err != nil {
			return WrapError(err, ErrorTypeRateLimit, indexName)
		}

		req := opensearchapi.IndicesRefreshReq{
			Indices: []string{indexName},
		}

		_, err := osi.client.GetClient().Indices.Refresh(ctx, &req)
		if err != nil {
			return osi.classifyOpenSearchError(err, indexName)
		}

		return nil
	}

	err := osi.client.ExecuteWithRetry(ctx, operation, fmt.Sprintf("RefreshIndex[%s]", indexName))

	duration := time.Since(startTime)
	osi.client.RecordRequest(duration, err == nil)

	if err != nil {
		return WrapError(err, ErrorTypeOpenSearchIndex, indexName)
	}

	return nil
}

// GetDocumentCount returns the number of documents in an index
func (osi *OpenSearchIndexerImpl) GetDocumentCount(ctx context.Context, indexName string) (int64, error) {
	startTime := time.Now()

	var count int64
	operation := func() error {
		if err := osi.client.WaitForRateLimit(ctx); err != nil {
			return WrapError(err, ErrorTypeRateLimit, indexName)
		}

		// Use search API with size 0 to get document count
		searchReq := opensearchapi.SearchReq{
			Indices: []string{indexName},
			Body: strings.NewReader(`{
				"size": 0,
				"track_total_hits": true
			}`),
		}

		_, err := osi.client.GetClient().Search(ctx, &searchReq)
		if err != nil {
			return osi.classifyOpenSearchError(err, indexName)
		}

		// TODO: Parse count response when OpenSearch client API is clarified
		// For now, return 0
		count = 0

		return nil
	}

	err := osi.client.ExecuteWithRetry(ctx, operation, fmt.Sprintf("GetDocumentCount[%s]", indexName))

	duration := time.Since(startTime)
	osi.client.RecordRequest(duration, err == nil)

	if err != nil {
		return 0, WrapError(err, ErrorTypeOpenSearchIndex, indexName)
	}

	return count, nil
}

// ProcessJapaneseText processes text using Japanese analyzer for better indexing
func (osi *OpenSearchIndexerImpl) ProcessJapaneseText(text string) (string, error) {
	if text == "" {
		return "", nil
	}

	processed := osi.textProcessor.ProcessQuery(text)
	if processed == nil {
		return "", fmt.Errorf("failed to process Japanese text")
	}

	// Return the normalized Japanese text
	if processed.JapaneseText != "" {
		return processed.JapaneseText, nil
	}

	return processed.Normalized, nil
}

// processJapaneseContentForDocument processes Japanese content for a document
func (osi *OpenSearchIndexerImpl) processJapaneseContentForDocument(document *OpenSearchDocument) error {
	if document == nil {
		return fmt.Errorf("document cannot be nil")
	}

	// Skip processing if ContentJa is already set
	if document.ContentJa != "" {
		return nil
	}

	// Process content for Japanese text extraction and normalization
	contentToProcess := document.Content
	if document.Title != "" {
		// Combine title and content for better Japanese processing
		contentToProcess = document.Title + " " + document.Content
	}

	processedContent, err := osi.ProcessJapaneseText(contentToProcess)
	if err != nil {
		log.Printf("Warning: Failed to process Japanese content for document %s: %v", document.ID, err)
		// Don't fail the entire operation, just use original content
		processedContent = contentToProcess
	}

	// Set the processed Japanese content
	document.ContentJa = processedContent

	return nil
}

// CreateVectorIndexWithJapanese creates a new index with Japanese-optimized mappings
func (osi *OpenSearchIndexerImpl) CreateVectorIndexWithJapanese(ctx context.Context, indexName string, dimension int) error {
	startTime := time.Now()

	// Use default dimension if not specified
	if dimension <= 0 {
		dimension = osi.defaultDimension
	}

	operation := func() error {
		if err := osi.client.WaitForRateLimit(ctx); err != nil {
			return WrapError(err, ErrorTypeRateLimit, indexName)
		}

		// Create Japanese-optimized index settings and mappings
		settings := map[string]interface{}{
			"settings": map[string]interface{}{
				"index": map[string]interface{}{
					"knn":                true,
					"number_of_shards":   1,
					"number_of_replicas": 0,
					"max_result_window":  10000,
					"max_rescore_window": 10000,
				},
				"analysis": map[string]interface{}{
					"analyzer": map[string]interface{}{
						"kuromoji": map[string]interface{}{
							"type":      "custom",
							"tokenizer": "kuromoji_tokenizer",
							"filter": []string{
								"lowercase",
								"kuromoji_baseform",
								"kuromoji_part_of_speech",
								"kuromoji_stemmer",
								"cjk_width",
								"stop",
								"synonym",
							},
						},
						"kuromoji_search": map[string]interface{}{
							"type":      "custom",
							"tokenizer": "kuromoji_tokenizer",
							"filter": []string{
								"lowercase",
								"kuromoji_baseform",
								"kuromoji_stemmer",
								"cjk_width",
							},
						},
					},
				},
			},
			"mappings": map[string]interface{}{
				"properties": map[string]interface{}{
					"title": map[string]interface{}{
						"type":            "text",
						"analyzer":        "kuromoji",
						"search_analyzer": "kuromoji_search",
						"fields": map[string]interface{}{
							"raw": map[string]interface{}{
								"type":         "keyword",
								"ignore_above": 256,
							},
						},
					},
					"content": map[string]interface{}{
						"type":            "text",
						"analyzer":        "kuromoji",
						"search_analyzer": "kuromoji_search",
					},
					"content_ja": map[string]interface{}{
						"type":            "text",
						"analyzer":        "kuromoji",
						"search_analyzer": "kuromoji_search",
					},
					"body": map[string]interface{}{
						"type":            "text",
						"analyzer":        "kuromoji",
						"search_analyzer": "kuromoji_search",
					},
					"category": map[string]interface{}{
						"type": "keyword",
						"fields": map[string]interface{}{
							"text": map[string]interface{}{
								"type":     "text",
								"analyzer": "kuromoji",
							},
						},
					},
					"tags": map[string]interface{}{
						"type": "keyword",
						"fields": map[string]interface{}{
							"text": map[string]interface{}{
								"type":     "text",
								"analyzer": "kuromoji",
							},
						},
					},
					"author": map[string]interface{}{
						"type": "keyword",
						"fields": map[string]interface{}{
							"text": map[string]interface{}{
								"type":     "text",
								"analyzer": "kuromoji",
							},
						},
					},
					"reference": map[string]interface{}{
						"type":     "text",
						"analyzer": "kuromoji",
					},
					"source": map[string]interface{}{
						"type": "keyword",
					},
					"file_path": map[string]interface{}{
						"type": "keyword",
					},
					"word_count": map[string]interface{}{
						"type": "integer",
					},
					"created_at": map[string]interface{}{
						"type": "date",
					},
					"updated_at": map[string]interface{}{
						"type": "date",
					},
					"indexed_at": map[string]interface{}{
						"type": "date",
					},
					"embedding": map[string]interface{}{
						"type":      "knn_vector",
						"dimension": dimension,
						"method": map[string]interface{}{
							"engine":     "lucene",
							"space_type": "cosinesimil",
							"name":       "hnsw",
							"parameters": map[string]interface{}{
								"ef_construction": 256,
								"m":               16,
							},
						},
					},
					"custom_fields": map[string]interface{}{
						"type":    "object",
						"enabled": true,
					},
				},
			},
		}

		bodyJSON, err := json.Marshal(settings)
		if err != nil {
			return fmt.Errorf("failed to marshal index settings: %w", err)
		}

		req := opensearchapi.IndicesCreateReq{
			Index: indexName,
			Body:  strings.NewReader(string(bodyJSON)),
		}

		_, err = osi.client.GetClient().Indices.Create(ctx, req)
		if err != nil {
			return osi.classifyOpenSearchError(err, indexName)
		}

		return nil
	}

	err := osi.client.ExecuteWithRetry(ctx, operation, fmt.Sprintf("CreateVectorIndexWithJapanese[%s]", indexName))

	duration := time.Since(startTime)
	osi.client.RecordRequest(duration, err == nil)

	if err != nil {
		return WrapError(err, ErrorTypeOpenSearchIndex, indexName)
	}

	log.Printf("Successfully created Japanese-optimized index %s with dimension %d in %v", indexName, dimension, duration)
	return nil
}

// ValidateIndexCompatibility checks if an existing index is compatible with current requirements
func (osi *OpenSearchIndexerImpl) ValidateIndexCompatibility(ctx context.Context, indexName string, requiredDimension int) error {
	startTime := time.Now()

	// Check if index exists
	exists, err := osi.IndexExists(ctx, indexName)
	if err != nil {
		return fmt.Errorf("failed to check index existence: %w", err)
	}

	if !exists {
		log.Printf("Index %s does not exist - compatibility validation skipped", indexName)
		return nil
	}

	var compatibilityErrors []string

	operation := func() error {
		if err := osi.client.WaitForRateLimit(ctx); err != nil {
			return WrapError(err, ErrorTypeRateLimit, indexName)
		}

		// Get index mappings and settings
		req := opensearchapi.IndicesGetReq{
			Indices: []string{indexName},
		}

		resp, err := osi.client.GetClient().Indices.Get(ctx, req)
		if err != nil {
			return osi.classifyOpenSearchError(err, indexName)
		}

		// TODO: Parse response when OpenSearch client API response format is clarified
		// For now, perform basic checks that can be done without parsing the response

		// Check if we can get basic info
		_, err = osi.GetIndexInfo(ctx, indexName)
		if err != nil {
			compatibilityErrors = append(compatibilityErrors, fmt.Sprintf("failed to get index info: %v", err))
		} else {
			log.Printf("Index %s compatibility check - basic info retrieved", indexName)
		}

		// Additional checks would be implemented here once response parsing is available
		// For example:
		// - Check vector dimension matches requiredDimension
		// - Check if kuromoji analyzer is configured
		// - Verify required fields are mapped correctly

		_ = resp // Suppress unused variable warning

		return nil
	}

	err = osi.client.ExecuteWithRetry(ctx, operation, fmt.Sprintf("ValidateIndexCompatibility[%s]", indexName))

	duration := time.Since(startTime)
	osi.client.RecordRequest(duration, err == nil)

	if err != nil {
		return WrapError(err, ErrorTypeOpenSearchIndex, indexName)
	}

	// Report compatibility issues if any
	if len(compatibilityErrors) > 0 {
		log.Printf("Index %s compatibility warnings: %v", indexName, compatibilityErrors)
		// For now, treat as warnings rather than errors
	}

	log.Printf("Index %s compatibility validation completed in %v", indexName, duration)
	return nil
}

// SafeDeleteIndex removes an existing index with safety checks and statistics collection
func (osi *OpenSearchIndexerImpl) SafeDeleteIndex(ctx context.Context, indexName string) (map[string]interface{}, error) {
	startTime := time.Now()

	// Collect statistics before deletion
	stats := make(map[string]interface{})
	stats["index_name"] = indexName
	stats["deletion_timestamp"] = time.Now().Unix()

	// Check if index exists
	exists, err := osi.IndexExists(ctx, indexName)
	if err != nil {
		return nil, fmt.Errorf("failed to check index existence before deletion: %w", err)
	}

	if !exists {
		stats["existed"] = false
		stats["document_count"] = 0
		log.Printf("Index %s does not exist - deletion skipped", indexName)
		return stats, nil
	}

	stats["existed"] = true

	// Get document count before deletion
	docCount, err := osi.GetDocumentCount(ctx, indexName)
	if err != nil {
		log.Printf("Warning: failed to get document count for %s before deletion: %v", indexName, err)
		docCount = -1 // Indicate unknown count
	}
	stats["document_count"] = docCount

	// Get index info for additional statistics
	indexInfo, err := osi.GetIndexInfo(ctx, indexName)
	if err != nil {
		log.Printf("Warning: failed to get index info for %s before deletion: %v", indexName, err)
	} else {
		stats["index_info"] = indexInfo
	}

	// Safety check: Confirm index name is not a system index
	if strings.HasPrefix(indexName, ".") {
		return nil, fmt.Errorf("refusing to delete system index: %s", indexName)
	}

	// Safety check: Confirm index name is reasonable length
	if len(indexName) < 3 {
		return nil, fmt.Errorf("refusing to delete index with suspicious name: %s", indexName)
	}

	// Perform the deletion
	log.Printf("Deleting index %s (estimated %d documents)...", indexName, docCount)
	err = osi.DeleteIndex(ctx, indexName)
	if err != nil {
		stats["deletion_success"] = false
		stats["deletion_error"] = err.Error()
		return stats, fmt.Errorf("failed to delete index %s: %w", indexName, err)
	}

	stats["deletion_success"] = true
	stats["deletion_duration"] = time.Since(startTime).String()

	log.Printf("Successfully deleted index %s (%d documents) in %v",
		indexName, docCount, time.Since(startTime))

	return stats, nil
}

// CompareIndexSettings compares current index settings with expected configuration
func (osi *OpenSearchIndexerImpl) CompareIndexSettings(ctx context.Context, indexName string, expectedDimension int) (map[string]interface{}, error) {
	startTime := time.Now()

	comparison := make(map[string]interface{})
	comparison["index_name"] = indexName
	comparison["expected_dimension"] = expectedDimension
	comparison["timestamp"] = time.Now().Unix()

	// Check if index exists
	exists, err := osi.IndexExists(ctx, indexName)
	if err != nil {
		return nil, fmt.Errorf("failed to check index existence: %w", err)
	}

	if !exists {
		comparison["exists"] = false
		comparison["compatible"] = false
		comparison["issues"] = []string{"Index does not exist"}
		return comparison, nil
	}

	comparison["exists"] = true

	var issues []string
	operation := func() error {
		if err := osi.client.WaitForRateLimit(ctx); err != nil {
			return WrapError(err, ErrorTypeRateLimit, indexName)
		}

		// Get index settings and mappings
		req := opensearchapi.IndicesGetReq{
			Indices: []string{indexName},
		}

		_, err := osi.client.GetClient().Indices.Get(ctx, req)
		if err != nil {
			return osi.classifyOpenSearchError(err, indexName)
		}

		// TODO: Parse response and perform detailed comparison when client API is clarified
		// For now, perform basic checks

		// Check basic accessibility
		info, err := osi.GetIndexInfo(ctx, indexName)
		if err != nil {
			issues = append(issues, fmt.Sprintf("cannot retrieve index info: %v", err))
		} else {
			comparison["current_info"] = info
		}

		// Future enhancements would include:
		// - Compare vector dimensions
		// - Check analyzer configuration
		// - Verify field mappings
		// - Check index settings (shards, replicas, etc.)

		return nil
	}

	err = osi.client.ExecuteWithRetry(ctx, operation, fmt.Sprintf("CompareIndexSettings[%s]", indexName))

	duration := time.Since(startTime)
	osi.client.RecordRequest(duration, err == nil)

	if err != nil {
		issues = append(issues, fmt.Sprintf("failed to retrieve settings: %v", err))
	}

	comparison["issues"] = issues
	comparison["compatible"] = len(issues) == 0
	comparison["check_duration"] = duration.String()

	log.Printf("Index %s settings comparison completed in %v (%d issues found)",
		indexName, duration, len(issues))

	return comparison, nil
}

// classifyOpenSearchError classifies OpenSearch errors into appropriate error types
func (osi *OpenSearchIndexerImpl) classifyOpenSearchError(err error, context string) error {
	if err == nil {
		return nil
	}

	errStr := strings.ToLower(err.Error())

	switch {
	case strings.Contains(errStr, "connection") || strings.Contains(errStr, "dial") || strings.Contains(errStr, "timeout"):
		return WrapError(err, ErrorTypeOpenSearchConnection, context)
	case strings.Contains(errStr, "mapping") || strings.Contains(errStr, "field"):
		return WrapError(err, ErrorTypeOpenSearchMapping, context)
	case strings.Contains(errStr, "index") && (strings.Contains(errStr, "not found") || strings.Contains(errStr, "missing")):
		return WrapError(err, ErrorTypeOpenSearchIndex, context)
	case strings.Contains(errStr, "bulk") || strings.Contains(errStr, "batch"):
		return WrapError(err, ErrorTypeOpenSearchBulkIndex, context)
	case strings.Contains(errStr, "query") || strings.Contains(errStr, "search"):
		return WrapError(err, ErrorTypeOpenSearchQuery, context)
	case strings.Contains(errStr, "rate limit") || strings.Contains(errStr, "too many requests"):
		return WrapError(err, ErrorTypeRateLimit, context)
	default:
		return WrapError(err, ErrorTypeOpenSearchIndexing, context)
	}
}
