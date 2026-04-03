package pdf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeOCRPrompt(t *testing.T) {
	tests := []struct {
		name         string
		customPrompt string
		expected     string
	}{
		{
			name:         "empty_string",
			customPrompt: "",
			expected:     ocrPrompt,
		},
		{
			name:         "whitespace_only",
			customPrompt: "   \n  ",
			expected:     ocrPrompt,
		},
		{
			name:         "non_empty",
			customPrompt: "Custom instructions",
			expected:     ocrPrompt + "\n\nCustom instructions",
		},
		{
			name:         "trailing_newlines",
			customPrompt: "Custom\n\n",
			expected:     ocrPrompt + "\n\nCustom",
		},
		{
			name:         "crlf_input",
			customPrompt: "Line1\r\nLine2",
			expected:     ocrPrompt + "\n\nLine1\nLine2",
		},
		{
			name:         "japanese_text",
			customPrompt: "このドキュメントの要約を詳細に記述してください",
			expected:     ocrPrompt + "\n\nこのドキュメントの要約を詳細に記述してください",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := composeOCRPrompt(tc.customPrompt)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestLoadOCRPromptFile(t *testing.T) {
	tests := []struct {
		name           string
		fileName       string
		content        string
		createFile     bool
		expected       string
		expectedErrMsg string
	}{
		{
			name:       "valid_file",
			fileName:   "prompt.txt",
			content:    "Hello world",
			createFile: true,
			expected:   "Hello world",
		},
		{
			name:           "missing_file",
			fileName:       "missing.txt",
			createFile:     false,
			expectedErrMsg: "missing.txt",
		},
		{
			name:       "empty_file",
			fileName:   "empty.txt",
			content:    "",
			createFile: true,
			expected:   "",
		},
		{
			name:       "whitespace_only_file",
			fileName:   "whitespace.txt",
			content:    "   \n  ",
			createFile: true,
			expected:   "",
		},
		{
			name:       "crlf_file",
			fileName:   "crlf.txt",
			content:    "Line1\r\nLine2\r\n",
			createFile: true,
			expected:   "Line1\nLine2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			filePath := filepath.Join(tempDir, tc.fileName)

			if tc.createFile {
				err := os.WriteFile(filePath, []byte(tc.content), 0o600)
				require.NoError(t, err)
			}

			result, err := LoadOCRPromptFile(filePath)
			if tc.expectedErrMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), filePath)
				assert.Contains(t, err.Error(), tc.expectedErrMsg)
				assert.Empty(t, result)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}
