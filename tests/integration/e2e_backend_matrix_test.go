package integration

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/stretchr/testify/require"

	"github.com/ca-srg/ragent/internal/ingestion/pdf"
	"github.com/ca-srg/ragent/internal/ingestion/vectorizer"
	appconfig "github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/pkg/opensearch"
)

type backendTestCase struct {
	name            string
	vectorDBBackend string
	useBearerToken  bool
	ocrProvider     string
}

var backendMatrix = []backendTestCase{
	{name: "s3-iam", vectorDBBackend: "s3", useBearerToken: false, ocrProvider: ""},
	{name: "s3-bearer", vectorDBBackend: "s3", useBearerToken: true, ocrProvider: ""},
	{name: "sqlite-iam", vectorDBBackend: "sqlite", useBearerToken: false, ocrProvider: ""},
	{name: "sqlite-bearer", vectorDBBackend: "sqlite", useBearerToken: true, ocrProvider: ""},
	{name: "s3-iam-gemini", vectorDBBackend: "s3", useBearerToken: false, ocrProvider: "gemini"},
	{name: "s3-iam-bedrock", vectorDBBackend: "s3", useBearerToken: false, ocrProvider: "bedrock"},
	{name: "s3-bearer-gemini", vectorDBBackend: "s3", useBearerToken: true, ocrProvider: "gemini"},
	{name: "s3-bearer-bedrock", vectorDBBackend: "s3", useBearerToken: true, ocrProvider: "bedrock"},
	{name: "sqlite-iam-gemini", vectorDBBackend: "sqlite", useBearerToken: false, ocrProvider: "gemini"},
	{name: "sqlite-iam-bedrock", vectorDBBackend: "sqlite", useBearerToken: false, ocrProvider: "bedrock"},
	{name: "sqlite-bearer-gemini", vectorDBBackend: "sqlite", useBearerToken: true, ocrProvider: "gemini"},
	{name: "sqlite-bearer-bedrock", vectorDBBackend: "sqlite", useBearerToken: true, ocrProvider: "bedrock"},
}

func skipIfEnvMissing(t *testing.T, key string) string {
	t.Helper()

	val := os.Getenv(key)
	if val == "" {
		t.Skipf("skipping: %s not set", key)
	}

	return val
}

func setupBackendEnv(t *testing.T, tc backendTestCase) {
	t.Helper()

	t.Setenv("VECTOR_DB_BACKEND", tc.vectorDBBackend)
	t.Setenv("OCR_PROVIDER", tc.ocrProvider)

	if tc.vectorDBBackend == "sqlite" {
		t.Setenv("SQLITE_VEC_DB_PATH", filepath.Join(t.TempDir(), "test-vectors.db"))
		t.Setenv("AWS_S3_VECTOR_BUCKET", "")
		t.Setenv("AWS_S3_VECTOR_INDEX", "")
	} else {
		t.Setenv("SQLITE_VEC_DB_PATH", "")
		t.Setenv("AWS_S3_VECTOR_BUCKET", skipIfEnvMissing(t, "AWS_S3_VECTOR_BUCKET"))
		t.Setenv("AWS_S3_VECTOR_INDEX", skipIfEnvMissing(t, "AWS_S3_VECTOR_INDEX"))
	}

	if tc.useBearerToken {
		val := skipIfEnvMissing(t, "AWS_BEARER_TOKEN_BEDROCK")
		t.Setenv("AWS_BEARER_TOKEN_BEDROCK", val)
	} else {
		t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	}

	if tc.ocrProvider == "gemini" {
		val := skipIfEnvMissing(t, "GEMINI_API_KEY")
		t.Setenv("GEMINI_API_KEY", val)
	} else {
		t.Setenv("GEMINI_API_KEY", "")
	}
}

func createE2EBedrockClient(t *testing.T, cfg *appconfig.Config) *bedrock.BedrockClient {
	t.Helper()

	if cfg.BedrockBearerToken != "" {
		awsCfg, err := bedrock.BuildBedrockAWSConfig(context.Background(), cfg.BedrockRegion, cfg.BedrockBearerToken)
		require.NoError(t, err, "failed to load AWS config")

		return bedrock.NewBedrockClient(awsCfg, "")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.S3VectorRegion),
	)
	require.NoError(t, err, "failed to load AWS config")

	return bedrock.NewBedrockClient(awsCfg, "")
}

func uniqueIndexName(tc backendTestCase) string {
	return "ragent-e2e-" + tc.name
}

func TestE2E_BackendMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	if os.Getenv("E2E_MATRIX_ENABLED") != "true" {
		t.Skip("set E2E_MATRIX_ENABLED=true to run backend matrix tests")
	}

	requireEnv(t, "OPENSEARCH_ENDPOINT")
	requireEnv(t, "OPENSEARCH_INDEX")

	for _, tc := range backendMatrix {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			setupBackendEnv(t, tc)

			cfg := loadE2EConfig(t)

			ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
			defer cancel()

			embeddingClient := createE2EBedrockClient(t, cfg)

			emb, err := embeddingClient.GenerateEmbedding(ctx, "E2E backend matrix test: "+tc.name)
			require.NoError(t, err)
			require.NotEmpty(t, emb)

			sf := vectorizer.NewServiceFactory(cfg)
			vectorStore, err := sf.CreateVectorStore()
			require.NoError(t, err)

			require.NoError(t, vectorStore.ValidateAccess(ctx))

			testVectorID := fmt.Sprintf("e2e-matrix-%s-%d", tc.name, time.Now().UnixNano())
			err = vectorStore.StoreVector(ctx, &vectorizer.VectorData{
				ID:        testVectorID,
				Embedding: emb,
				Content:   "E2E backend matrix test: " + tc.name,
				Metadata: vectorizer.DocumentMetadata{
					Title:    "E2E Matrix Test: " + tc.name,
					Category: "e2e-test",
					Source:   "e2e-matrix",
				},
				CreatedAt: time.Now(),
			})
			require.NoError(t, err)

			backendInfo, err := vectorStore.GetBackendInfo(ctx)
			require.NoError(t, err)
			require.NotNil(t, backendInfo)

			osClient := createE2EOpenSearchClient(t, cfg)

			indexName := uniqueIndexName(tc)
			createMatrixTestIndex(t, cfg.OpenSearchEndpoint, indexName)

			t.Cleanup(func() {
				deleteMatrixTestIndex(t, cfg.OpenSearchEndpoint, indexName)
				if tc.vectorDBBackend == "s3" {
					_ = vectorStore.DeleteVector(context.Background(), testVectorID)
				}
			})

			testDocs := buildMatrixTestDocs(tc.name)
			for _, doc := range testDocs {
				docEmb, embErr := embeddingClient.GenerateEmbedding(ctx, doc["content"].(string))
				require.NoError(t, embErr)
				doc["embedding"] = docEmb
				require.NoError(t, osClient.IndexDocument(ctx, indexName, doc["id"].(string), doc))
			}

			refreshURL := fmt.Sprintf("%s/%s/_refresh", cfg.OpenSearchEndpoint, indexName)
			resp, err := http.Post(refreshURL, "application/json", nil)
			require.NoError(t, err)
			_ = resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)

			hybridEngine := opensearch.NewHybridSearchEngine(osClient, embeddingClient)
			searchResult, err := hybridEngine.Search(ctx, &opensearch.HybridQuery{
				Query:          "matrix test",
				IndexName:      indexName,
				Size:           5,
				BM25Weight:     0.5,
				VectorWeight:   0.5,
				FusionMethod:   opensearch.FusionMethodRRF,
				UseJapaneseNLP: false,
				TimeoutSeconds: 30,
			})
			require.NoError(t, err)
			require.NotNil(t, searchResult)
			require.NotNil(t, searchResult.FusionResult)

			if tc.ocrProvider != "" {
				runOCRVerification(t, ctx, tc, cfg, embeddingClient, osClient, indexName)
			}

			t.Logf("✅ Pattern %s: embedding(%d dims) + %s vector store + OpenSearch + hybrid search OK",
				tc.name, len(emb), tc.vectorDBBackend)
		})
	}
}

func createMatrixTestIndex(t *testing.T, endpoint, indexName string) {
	t.Helper()

	mappingPath := filepath.Join("..", "testdata", "e2e-index-mapping.json")
	mappingData, err := os.ReadFile(mappingPath)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPut, endpoint+"/"+indexName, bytes.NewReader(mappingData))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Contains(t, []int{http.StatusOK, http.StatusBadRequest}, resp.StatusCode)
}

func deleteMatrixTestIndex(t *testing.T, endpoint, indexName string) {
	t.Helper()

	req, err := http.NewRequest(http.MethodDelete, endpoint+"/"+indexName, nil)
	if err != nil {
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = resp.Body.Close() }()
}

func buildMatrixTestDocs(patternName string) []map[string]interface{} {
	return []map[string]interface{}{
		{
			"id":       "matrix-doc-1-" + patternName,
			"title":    "Backend Matrix Primary Document: " + patternName,
			"content":  "Matrix test content for pattern " + patternName + " covering embedding generation, vector storage, and hybrid search.",
			"category": "e2e-test",
			"source":   "e2e-matrix",
		},
		{
			"id":       "matrix-doc-2-" + patternName,
			"title":    "Backend Matrix Secondary Document: " + patternName,
			"content":  "Secondary matrix test document for pattern " + patternName + " to verify OpenSearch indexing and retrieval.",
			"category": "e2e-test",
			"source":   "e2e-matrix",
		},
	}
}

func runOCRVerification(
	t *testing.T,
	ctx context.Context,
	tc backendTestCase,
	cfg *appconfig.Config,
	embeddingClient *bedrock.BedrockClient,
	osClient *opensearch.Client,
	indexName string,
) {
	t.Helper()

	pdfPath := filepath.Join("..", "testdata", "e2e-test-sample.pdf")
	pdfData, err := os.ReadFile(pdfPath)
	require.NoError(t, err)

	var pages []*pdf.PageResult

	switch tc.ocrProvider {
	case "bedrock":
		if cfg.BedrockBearerToken != "" {
			awsCfg, cfgErr := bedrock.BuildBedrockAWSConfig(ctx, cfg.BedrockRegion, cfg.BedrockBearerToken)
			require.NoError(t, cfgErr)

			ocrClient, clientErr := pdf.NewBedrockOCRClient(awsCfg, cfg.OCRModel, cfg.OCRTimeout, 0, 0)
			require.NoError(t, clientErr)

			pages, err = ocrClient.ExtractPages(ctx, pdfData, "e2e-test-sample.pdf")
		} else {
			awsCfg, cfgErr := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.BedrockRegion))
			require.NoError(t, cfgErr)

			ocrClient, clientErr := pdf.NewBedrockOCRClient(awsCfg, cfg.OCRModel, cfg.OCRTimeout, 0, 0)
			require.NoError(t, clientErr)

			pages, err = ocrClient.ExtractPages(ctx, pdfData, "e2e-test-sample.pdf")
		}
	case "gemini":
		ocrClient, clientErr := pdf.NewGeminiOCRClient(cfg.GeminiAPIKey, cfg.OCRModel, cfg.OCRTimeout, 0, 0)
		require.NoError(t, clientErr)

		pages, err = ocrClient.ExtractPages(ctx, pdfData, "e2e-test-sample.pdf")
	default:
		t.Fatalf("unsupported OCR provider: %s", tc.ocrProvider)
	}

	require.NoError(t, err)
	require.NotEmpty(t, pages)
	require.NotEmpty(t, pages[0].Text)

	ocrText := pages[0].Text
	ocrEmbedding, err := embeddingClient.GenerateEmbedding(ctx, ocrText)
	require.NoError(t, err)

	ocrDocID := fmt.Sprintf("ocr-%s-%d", tc.name, time.Now().UnixNano())
	ocrDoc := map[string]interface{}{
		"id":        ocrDocID,
		"title":     "OCR Matrix Document: " + tc.name,
		"content":   ocrText,
		"category":  "e2e-ocr",
		"source":    "e2e-matrix-ocr",
		"embedding": ocrEmbedding,
	}
	require.NoError(t, osClient.IndexDocument(ctx, indexName, ocrDocID, ocrDoc))

	refreshURL := fmt.Sprintf("%s/%s/_refresh", cfg.OpenSearchEndpoint, indexName)
	resp, err := http.Post(refreshURL, "application/json", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	hybridEngine := opensearch.NewHybridSearchEngine(osClient, embeddingClient)
	searchResult, err := hybridEngine.Search(ctx, &opensearch.HybridQuery{
		Query:          ocrText,
		IndexName:      indexName,
		Size:           5,
		BM25Weight:     0.5,
		VectorWeight:   0.5,
		FusionMethod:   opensearch.FusionMethodRRF,
		UseJapaneseNLP: false,
		TimeoutSeconds: 30,
	})
	require.NoError(t, err)
	require.NotNil(t, searchResult)
	require.NotNil(t, searchResult.FusionResult)
}
