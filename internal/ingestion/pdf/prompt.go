package pdf

import (
	"fmt"
	"os"
	"strings"
)

func composeOCRPrompt(customPrompt string) string {
	normalizedPrompt := normalizeOCRPromptLineEndings(customPrompt)
	trimmedPrompt := strings.TrimSpace(normalizedPrompt)
	if trimmedPrompt == "" {
		return ocrPrompt
	}

	return ocrPrompt + "\n\n" + trimmedPrompt
}

func LoadOCRPromptFile(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to load OCR prompt file %s: %w", filePath, err)
	}

	normalizedContent := normalizeOCRPromptLineEndings(string(content))
	trimmedContent := strings.TrimSpace(normalizedContent)
	if trimmedContent == "" {
		return "", nil
	}

	return trimmedContent, nil
}

func normalizeOCRPromptLineEndings(value string) string {
	return strings.ReplaceAll(value, "\r\n", "\n")
}
