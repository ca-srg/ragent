package embedding

import "context"

type EmbeddingClient interface {
	GenerateEmbedding(ctx context.Context, text string) ([]float64, error)
	ValidateConnection(ctx context.Context) error
	GetModelInfo() (string, int, error)
}
