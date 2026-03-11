package embedding

import (
	"context"
	"fmt"

	"github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/pkg/embedding/gemini"
)

func NewEmbeddingClient(cfg *config.Config) (EmbeddingClient, error) {
	ctx := context.Background()

	switch cfg.EmbeddingProvider {
	case "", "bedrock":
		awsCfg, err := bedrock.BuildBedrockAWSConfig(ctx, cfg.BedrockRegion, cfg.BedrockBearerToken)
		if err != nil {
			return nil, fmt.Errorf("failed to build AWS config for Bedrock embedding: %w", err)
		}

		return bedrock.NewBedrockClient(awsCfg, cfg.EmbeddingModel), nil
	case "gemini":
		return gemini.NewGeminiEmbeddingClient(
			cfg.GeminiAPIKey,
			cfg.GeminiGCPProject,
			cfg.GeminiGCPLocation,
			cfg.EmbeddingModel,
			cfg.EmbeddingDimension,
		)
	default:
		return nil, fmt.Errorf(
			"unsupported embedding provider: %q (supported: bedrock, gemini)",
			cfg.EmbeddingProvider,
		)
	}
}
