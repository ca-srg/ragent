package pdf

import (
	"context"
	"log"

	"github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
)

func NewReaderFromConfig(cfg *config.Config) *Reader {
	switch cfg.OCRProvider {
	case "bedrock":
		awsCfg, err := bedrock.BuildBedrockAWSConfig(context.TODO(), cfg.BedrockRegion, cfg.BedrockBearerToken)
		if err != nil {
			log.Printf("Warning: failed to build AWS config for OCR: %v, PDF files will be skipped", err)
			return nil
		}
		ocrClient, err := NewBedrockOCRClient(awsCfg, cfg.OCRModel, cfg.OCRTimeout, cfg.OCRMaxTokens, cfg.OCRConcurrency)
		if err != nil {
			log.Printf("Warning: failed to create Bedrock OCR client: %v, PDF files will be skipped", err)
			return nil
		}
		return newReaderWithLog(ocrClient, cfg)

	case "gemini":
		ocrClient, err := NewGeminiOCRClient(
			cfg.GeminiAPIKey, cfg.GeminiGCPProject, cfg.GeminiGCPLocation,
			cfg.OCRModel, cfg.OCRTimeout, cfg.OCRMaxTokens, cfg.OCRConcurrency,
		)
		if err != nil {
			log.Printf("Warning: failed to create Gemini OCR client: %v, PDF files will be skipped", err)
			return nil
		}
		return newReaderWithLog(ocrClient, cfg)

	case "":
		return nil

	default:
		log.Printf("Warning: unsupported OCR_PROVIDER=%q, PDF files will be skipped", cfg.OCRProvider)
		return nil
	}
}

func newReaderWithLog(client OCRClient, cfg *config.Config) *Reader {
	log.Printf("PDF OCR enabled: provider=%s, model=%s", cfg.OCRProvider, cfg.OCRModel)
	return NewReader(client, PDFReaderConfig{
		Provider:    cfg.OCRProvider,
		Model:       cfg.OCRModel,
		Timeout:     cfg.OCRTimeout,
		Concurrency: cfg.OCRConcurrency,
	})
}
