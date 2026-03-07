package cmd

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"

	appconfig "github.com/ca-srg/ragent/internal/pkg/config"
	queryimpl "github.com/ca-srg/ragent/internal/query"
)

type QueryDependencyOverrides struct {
	LoadConfig           queryimpl.AppConfigLoader
	LoadAWSConfig        queryimpl.AWSConfigLoader
	LoadBedrockAWSConfig queryimpl.BedrockAWSConfigBuilder
	NewEmbeddingClient   queryimpl.BedrockClientFactory
	NewOpenSearchClient  queryimpl.OpenSearchClientFactory
	NewHybridEngine      queryimpl.HybridEngineFactory
}

func OverrideQueryDependencies(overrides QueryDependencyOverrides) func() {
	prevLoadConfig := queryimpl.LoadAppConfig
	prevLoadAWS := queryimpl.LoadAWSConfig
	prevLoadBedrockAWS := queryimpl.LoadBedrockAWSConfig
	prevEmbedding := queryimpl.NewEmbeddingClient
	prevOpenSearch := queryimpl.NewOpenSearchClient
	prevHybrid := queryimpl.NewHybridEngine

	if overrides.LoadConfig != nil {
		queryimpl.LoadAppConfig = overrides.LoadConfig
	}
	if overrides.LoadAWSConfig != nil {
		queryimpl.LoadAWSConfig = overrides.LoadAWSConfig
	}
	if overrides.LoadBedrockAWSConfig != nil {
		queryimpl.LoadBedrockAWSConfig = overrides.LoadBedrockAWSConfig
	}
	if overrides.NewEmbeddingClient != nil {
		queryimpl.NewEmbeddingClient = overrides.NewEmbeddingClient
	}
	if overrides.NewOpenSearchClient != nil {
		queryimpl.NewOpenSearchClient = overrides.NewOpenSearchClient
	}
	if overrides.NewHybridEngine != nil {
		queryimpl.NewHybridEngine = overrides.NewHybridEngine
	}

	return func() {
		queryimpl.LoadAppConfig = prevLoadConfig
		queryimpl.LoadAWSConfig = prevLoadAWS
		queryimpl.LoadBedrockAWSConfig = prevLoadBedrockAWS
		queryimpl.NewEmbeddingClient = prevEmbedding
		queryimpl.NewOpenSearchClient = prevOpenSearch
		queryimpl.NewHybridEngine = prevHybrid
	}
}

// Helpers to build default override closures without importing internal types in tests.
func DefaultLoadConfigOverride(cfg *appconfig.Config, err error) queryimpl.AppConfigLoader {
	return func() (*appconfig.Config, error) {
		return cfg, err
	}
}

func DefaultAWSConfigOverride(cfg aws.Config, err error) queryimpl.AWSConfigLoader {
	return func(ctx context.Context, optFns ...func(*awsconfig.LoadOptions) error) (aws.Config, error) {
		return cfg, err
	}
}

func DefaultBedrockAWSConfigOverride(cfg aws.Config, err error) queryimpl.BedrockAWSConfigBuilder {
	return func(ctx context.Context, region, bearerToken string) (aws.Config, error) {
		return cfg, err
	}
}

func EmbeddingClientOverride(factory queryimpl.BedrockClientFactory) queryimpl.BedrockClientFactory {
	return factory
}

func OpenSearchClientOverride(factory queryimpl.OpenSearchClientFactory) queryimpl.OpenSearchClientFactory {
	return factory
}

func HybridEngineOverride(factory queryimpl.HybridEngineFactory) queryimpl.HybridEngineFactory {
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
	queryOnlySlack = false
	slackChannels = nil
}
