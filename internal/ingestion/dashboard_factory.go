package ingestion

import (
	"fmt"
	"log"

	"github.com/ca-srg/ragent/internal/ingestion/metadata"
	"github.com/ca-srg/ragent/internal/ingestion/pdf"
	"github.com/ca-srg/ragent/internal/ingestion/scanner"
	"github.com/ca-srg/ragent/internal/ingestion/vectorizer"
	appconfig "github.com/ca-srg/ragent/internal/pkg/config"
	pkgdomain "github.com/ca-srg/ragent/internal/pkg/domain"
	"github.com/ca-srg/ragent/internal/pkg/embedding"
)

// BuildDashboardDependencies constructs a FileScanner and Vectorizer for the
// web dashboard.  Keeping this factory inside the ingestion slice prevents
// other slices from importing ingestion sub-packages directly.
func BuildDashboardDependencies() (pkgdomain.FileScanner, pkgdomain.Vectorizer, error) {
	appCfg, err := appconfig.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	embeddingClient, err := embedding.NewEmbeddingClient(appCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create embedding client: %w", err)
	}

	sf := vectorizer.NewServiceFactory(appCfg)
	vectorStoreClient, err := sf.CreateVectorStore()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create vector store client: %w", err)
	}

	metadataExtractor := metadata.NewMetadataExtractor()
	fileScanner := scanner.NewFileScanner()

	var osIndexer vectorizer.OpenSearchIndexer
	if appCfg.OpenSearchEndpoint != "" {
		factory := vectorizer.NewIndexerFactory(appCfg)
		webDimension := 768
		if embeddingClient != nil {
			_, d, dErr := embeddingClient.GetModelInfo()
			if dErr == nil && d > 0 {
				webDimension = d
			}
		}
		osIndexer, err = factory.CreateOpenSearchIndexer(appCfg.OpenSearchIndex, webDimension)
		if err != nil {
			log.Printf("Warning: failed to create OpenSearch indexer: %v", err)
		}
	}

	serviceConfig := &vectorizer.ServiceConfig{
		Config:              appCfg,
		EmbeddingClient:     embeddingClient,
		VectorStoreClient:   vectorStoreClient,
		OpenSearchIndexer:   osIndexer,
		MetadataExtractor:   metadataExtractor,
		FileScanner:         fileScanner,
		EnableOpenSearch:    osIndexer != nil,
		OpenSearchIndexName: appCfg.OpenSearchIndex,
		PDFReader:           pdf.NewReaderFromConfig(appCfg),
	}

	vec, err := vectorizer.NewVectorizerService(serviceConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create vectorizer service: %w", err)
	}

	return fileScanner, vec, nil
}
