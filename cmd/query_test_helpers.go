package cmd

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"

	appconfig "github.com/ca-srg/ragent/internal/pkg/config"
)

type QueryDependencyOverrides struct {
	LoadConfig          appConfigLoader
	LoadAWSConfig       awsConfigLoader
	NewEmbeddingClient  bedrockClientFactory
	NewOpenSearchClient openSearchClientFactory
	NewHybridEngine     hybridEngineFactory
}

func OverrideQueryDependencies(overrides QueryDependencyOverrides) func() {
	prevLoadConfig := loadAppConfig
	prevLoadAWS := loadAWSConfig
	prevEmbedding := newEmbeddingClient
	prevOpenSearch := newOpenSearchClient
	prevHybrid := newHybridEngine

	if overrides.LoadConfig != nil {
		loadAppConfig = overrides.LoadConfig
	}
	if overrides.LoadAWSConfig != nil {
		loadAWSConfig = overrides.LoadAWSConfig
	}
	if overrides.NewEmbeddingClient != nil {
		newEmbeddingClient = overrides.NewEmbeddingClient
	}
	if overrides.NewOpenSearchClient != nil {
		newOpenSearchClient = overrides.NewOpenSearchClient
	}
	if overrides.NewHybridEngine != nil {
		newHybridEngine = overrides.NewHybridEngine
	}

	return func() {
		loadAppConfig = prevLoadConfig
		loadAWSConfig = prevLoadAWS
		newEmbeddingClient = prevEmbedding
		newOpenSearchClient = prevOpenSearch
		newHybridEngine = prevHybrid
	}
}

// Helpers to build default override closures without importing internal types in tests.
func DefaultLoadConfigOverride(cfg *appconfig.Config, err error) appConfigLoader {
	return func() (*appconfig.Config, error) {
		return cfg, err
	}
}

func DefaultAWSConfigOverride(cfg aws.Config, err error) awsConfigLoader {
	return func(ctx context.Context, optFns ...func(*awsconfig.LoadOptions) error) (aws.Config, error) {
		return cfg, err
	}
}

func EmbeddingClientOverride(factory bedrockClientFactory) bedrockClientFactory {
	return factory
}

func OpenSearchClientOverride(factory openSearchClientFactory) openSearchClientFactory {
	return factory
}

func HybridEngineOverride(factory hybridEngineFactory) hybridEngineFactory {
	return factory
}

func ResetQueryState() {
	queryText = ""
	topK = 10
	outputJSON = false
	filterQuery = ""
	searchMode = "hybrid"
	indexName = ""
	bm25Weight = 0.5
	vectorWeight = 0.5
	fusionMethod = "rrf"
	useJapaneseNLP = false
	timeout = 30
}
