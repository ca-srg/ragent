package s3vector

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors/document"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors/types"

	commontypes "github.com/ca-srg/ragent/internal/types"
)

// S3VectorService implements the S3VectorClient interface
type S3VectorService struct {
	client           *s3vectors.Client
	vectorBucketName string
	indexName        string
	region           string
}

// S3Config holds the configuration for S3 Vectors client
type S3Config struct {
	VectorBucketName string
	IndexName        string
	Region           string
}

// NewS3VectorService creates a new S3 Vectors service
func NewS3VectorService(cfg *S3Config) (*S3VectorService, error) {
	if cfg.VectorBucketName == "" {
		return nil, fmt.Errorf("vector bucket name is required")
	}
	if cfg.IndexName == "" {
		return nil, fmt.Errorf("index name is required")
	}

	// Create AWS config with default credential chain
	awsConfig, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 Vectors client
	s3VectorsClient := s3vectors.NewFromConfig(awsConfig)

	return &S3VectorService{
		client:           s3VectorsClient,
		vectorBucketName: cfg.VectorBucketName,
		indexName:        cfg.IndexName,
		region:           cfg.Region,
	}, nil
}

// StoreVector saves a vector with its metadata to S3 Vectors
func (s *S3VectorService) StoreVector(ctx context.Context, vectorData *commontypes.VectorData) error {
	if vectorData == nil {
		log.Printf("ERROR: vector data is nil")
		return fmt.Errorf("vector data cannot be nil")
	}

	if vectorData.ID == "" {
		log.Printf("ERROR: vector ID is empty")
		return fmt.Errorf("vector ID cannot be empty")
	}

	if vectorData.Embedding == nil {
		log.Printf("ERROR: vector embedding is nil for ID: %s", vectorData.ID)
		return fmt.Errorf("vector embedding cannot be nil")
	}

	if len(vectorData.Embedding) == 0 {
		log.Printf("ERROR: vector embedding is empty for ID: %s", vectorData.ID)
		return fmt.Errorf("vector embedding cannot be empty")
	}

	// Convert embedding to float32 slice
	float32Embedding := make([]float32, len(vectorData.Embedding))
	for i, v := range vectorData.Embedding {
		float32Embedding[i] = float32(v)
	}

	// Prepare metadata for S3 Vectors
	// NOTE: S3 Vectorsのフィルタ可能メタデータは最大2048バイト制限があるため
	// 大きい本文は入れず、短い抜粋のみを保存する
	metadataMap := map[string]interface{}{
		"title":      vectorData.Metadata.Title,
		"category":   vectorData.Metadata.Category,
		"file_path":  vectorData.Metadata.FilePath,
		"created_at": vectorData.CreatedAt.Format(time.RFC3339),
		"word_count": vectorData.Metadata.WordCount,
	}

	// Add short excerpt of content to avoid metadata size limits (<= 2048 bytes in total)
	if vectorData.Content != "" {
		excerpt := truncateUTF8ByBytes(vectorData.Content, 512) // up to 512 bytes excerpt
		if excerpt != "" {
			metadataMap["content_excerpt"] = excerpt
		}
	}

	// Add reference if available
	if vectorData.Metadata.Reference != "" {
		metadataMap["reference"] = vectorData.Metadata.Reference
	}

	// Add author if available
	if vectorData.Metadata.Author != "" {
		metadataMap["author"] = vectorData.Metadata.Author
	}

	// Add tags if available
	if len(vectorData.Metadata.Tags) > 0 {
		metadataMap["tags"] = vectorData.Metadata.Tags
	}

	// Add custom fields from metadata (including reference)
	for key, value := range vectorData.Metadata.CustomFields {
		metadataMap[key] = value
	}

	vectorMetadata := document.NewLazyDocument(metadataMap)

	// Upload vector to S3 Vectors
	_, err := s.client.PutVectors(ctx, &s3vectors.PutVectorsInput{
		VectorBucketName: aws.String(s.vectorBucketName),
		IndexName:        aws.String(s.indexName),
		Vectors: []types.PutInputVector{
			{
				Key: aws.String(vectorData.ID),
				Data: &types.VectorDataMemberFloat32{
					Value: float32Embedding,
				},
				Metadata: vectorMetadata,
			},
		},
	})

	if err != nil {
		return fmt.Errorf("failed to upload vector to S3 Vectors: %w", err)
	}

	return nil
}

// truncateUTF8ByBytes returns a string truncated so that its UTF-8 byte length
// does not exceed the specified limit. It preserves rune boundaries.
func truncateUTF8ByBytes(s string, limit int) string {
	if limit <= 0 || len(s) == 0 {
		return ""
	}
	// Fast path: already within limit
	if len([]byte(s)) <= limit {
		return s
	}
	// Walk runes and accumulate until exceeding limit
	var (
		total int
		end   int
	)
	for i, r := range s {
		// size in bytes of this rune in UTF-8
		var buf [4]byte
		n := copy(buf[:], string(r))
		if total+n > limit {
			end = i
			break
		}
		total += n
		end = i + n // i is byte index of rune start; but range over string gives byte index, so increment by n
	}
	if end <= 0 || end > len(s) {
		end = len(s)
	}
	return s[:end]
}

// ValidateAccess checks if S3 Vector bucket is accessible
func (s *S3VectorService) ValidateAccess(ctx context.Context) error {
	// Try to get the vector bucket
	_, err := s.client.GetVectorBucket(ctx, &s3vectors.GetVectorBucketInput{
		VectorBucketName: aws.String(s.vectorBucketName),
	})

	if err != nil {
		return fmt.Errorf("cannot access S3 bucket %s: %w", s.vectorBucketName, err)
	}

	// Also verify the index exists
	_, err = s.client.GetIndex(ctx, &s3vectors.GetIndexInput{
		VectorBucketName: aws.String(s.vectorBucketName),
		IndexName:        aws.String(s.indexName),
	})

	if err != nil {
		return fmt.Errorf("cannot access index %s in bucket %s: %w", s.indexName, s.vectorBucketName, err)
	}

	return nil
}

// ListVectors returns a list of stored vector keys
func (s *S3VectorService) ListVectors(ctx context.Context, prefix string) ([]string, error) {
	var keys []string

	// List vectors from S3 Vectors API
	result, err := s.client.ListVectors(ctx, &s3vectors.ListVectorsInput{
		VectorBucketName: aws.String(s.vectorBucketName),
		IndexName:        aws.String(s.indexName),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list vectors: %w", err)
	}

	for _, vector := range result.Vectors {
		if vector.Key != nil {
			keys = append(keys, *vector.Key)
		}
	}

	return keys, nil
}

// DeleteVector removes a vector from S3 Vectors
func (s *S3VectorService) DeleteVector(ctx context.Context, vectorID string) error {
	if vectorID == "" {
		return fmt.Errorf("vector ID cannot be empty")
	}

	// Delete vector using S3 Vectors API
	_, err := s.client.DeleteVectors(ctx, &s3vectors.DeleteVectorsInput{
		VectorBucketName: aws.String(s.vectorBucketName),
		IndexName:        aws.String(s.indexName),
		Keys:             []string{vectorID},
	})

	if err != nil {
		return fmt.Errorf("failed to delete vector %s from S3 Vectors: %w", vectorID, err)
	}

	return nil
}

// GetVector retrieves a vector from S3 Vectors
func (s *S3VectorService) GetVector(ctx context.Context, vectorID string) (*commontypes.VectorData, error) {
	if vectorID == "" {
		return nil, fmt.Errorf("vector ID cannot be empty")
	}

	// Get vector using S3 Vectors API
	result, err := s.client.GetVectors(ctx, &s3vectors.GetVectorsInput{
		VectorBucketName: aws.String(s.vectorBucketName),
		IndexName:        aws.String(s.indexName),
		Keys:             []string{vectorID},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to get vector %s from S3 Vectors: %w", vectorID, err)
	}

	if len(result.Vectors) == 0 {
		return nil, fmt.Errorf("vector %s not found", vectorID)
	}

	// Note: This is a simplified implementation
	// A full implementation would convert the vector data back to our internal format
	_ = result.Vectors[0] // Acknowledge we have the vector but don't use it yet
	return nil, fmt.Errorf("GetVector not fully implemented for S3 Vectors")
}

// BatchStoreVectors stores multiple vectors in a single operation
func (s *S3VectorService) BatchStoreVectors(ctx context.Context, vectors []*commontypes.VectorData) error {
	if len(vectors) == 0 {
		return nil
	}

	// For now, just call StoreVector for each vector
	// TODO: Optimize this to use bulk operations when needed
	for _, vector := range vectors {
		if err := s.StoreVector(ctx, vector); err != nil {
			return fmt.Errorf("failed to store vector %s: %w", vector.ID, err)
		}
	}

	return nil
}

// GetBucketInfo returns information about the S3 Vector bucket
func (s *S3VectorService) GetBucketInfo(ctx context.Context) (map[string]interface{}, error) {
	info := make(map[string]interface{})

	// Get vector bucket info
	_, err := s.client.GetVectorBucket(ctx, &s3vectors.GetVectorBucketInput{
		VectorBucketName: aws.String(s.vectorBucketName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get vector bucket info: %w", err)
	}

	info["vector_bucket_name"] = s.vectorBucketName
	info["index_name"] = s.indexName
	info["region"] = s.region

	return info, nil
}

// QueryVectors performs a similarity search using a query vector
func (s *S3VectorService) QueryVectors(ctx context.Context, queryVector []float64, topK int, filter map[string]interface{}) (*commontypes.QueryVectorsResult, error) {
	if len(queryVector) == 0 {
		return nil, fmt.Errorf("query vector cannot be empty")
	}

	if topK <= 0 {
		topK = 10 // default to 10 results
	}

	// Convert float64 to float32
	float32Vector := make([]float32, len(queryVector))
	for i, v := range queryVector {
		float32Vector[i] = float32(v)
	}

	// Prepare query input
	input := &s3vectors.QueryVectorsInput{
		VectorBucketName: aws.String(s.vectorBucketName),
		IndexName:        aws.String(s.indexName),
		QueryVector: &types.VectorDataMemberFloat32{
			Value: float32Vector,
		},
		TopK:           aws.Int32(int32(topK)),
		ReturnDistance: true,
		ReturnMetadata: true,
	}

	// Add filter if provided
	if len(filter) > 0 {
		// Convert map to document for S3 Vectors API
		filterDoc := document.NewLazyDocument(filter)
		input.Filter = filterDoc
	}

	// Execute query
	result, err := s.client.QueryVectors(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to query vectors: %w", err)
	}

	// Convert API result to our result format
	queryResult := &commontypes.QueryVectorsResult{
		Results:    make([]commontypes.QueryResult, 0, len(result.Vectors)),
		TotalCount: len(result.Vectors),
		TopK:       topK,
	}

	for _, vector := range result.Vectors {
		queryRes := commontypes.QueryResult{}

		if vector.Key != nil {
			queryRes.Key = *vector.Key
		}

		if vector.Distance != nil {
			queryRes.Distance = float64(*vector.Distance)
		}

		if vector.Metadata != nil {
			// Use UnmarshalSmithyDocument to extract metadata
			var metadata map[string]interface{}

			err := vector.Metadata.UnmarshalSmithyDocument(&metadata)
			if err != nil {
				queryRes.Metadata = make(map[string]interface{})
			} else {
				queryRes.Metadata = metadata
			}
		}

		queryResult.Results = append(queryResult.Results, queryRes)
	}

	return queryResult, nil
}

// DeleteAllVectors removes all vectors from S3 Vectors index
func (s *S3VectorService) DeleteAllVectors(ctx context.Context) (int, error) {
	// Get list of all vectors first
	keys, err := s.ListVectors(ctx, "")
	if err != nil {
		return 0, fmt.Errorf("failed to list vectors before deletion: %w", err)
	}

	if len(keys) == 0 {
		return 0, nil // Nothing to delete
	}

	// Delete all vectors in batch if S3 Vectors supports it
	_, err = s.client.DeleteVectors(ctx, &s3vectors.DeleteVectorsInput{
		VectorBucketName: aws.String(s.vectorBucketName),
		IndexName:        aws.String(s.indexName),
		Keys:             keys,
	})

	if err != nil {
		return 0, fmt.Errorf("failed to delete all vectors from S3 Vectors: %w", err)
	}

	return len(keys), nil
}
