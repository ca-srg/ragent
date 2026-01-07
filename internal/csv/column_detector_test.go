package csv

import (
	"testing"
)

func TestColumnDetector_DetectContentColumns(t *testing.T) {
	tests := []struct {
		name       string
		headers    []string
		sampleRows [][]string
		expected   []string
	}{
		{
			name:    "detect Japanese content column",
			headers: []string{"ID", "タイトル", "本文", "カテゴリ"},
			sampleRows: [][]string{
				{"1", "テスト", "これは本文です", "A"},
				{"2", "テスト2", "これも本文です", "B"},
			},
			expected: []string{"本文"},
		},
		{
			name:    "detect English content column",
			headers: []string{"id", "title", "content", "category"},
			sampleRows: [][]string{
				{"1", "Test", "This is content", "A"},
				{"2", "Test2", "More content here", "B"},
			},
			expected: []string{"content"},
		},
		{
			name:    "detect description column",
			headers: []string{"id", "name", "description"},
			sampleRows: [][]string{
				{"1", "Item", "A long description text that should be detected"},
				{"2", "Item2", "Another long description for testing"},
			},
			expected: []string{"description"},
		},
		{
			name:    "fallback to longest column",
			headers: []string{"short", "medium", "long"},
			sampleRows: [][]string{
				{"a", "abc", "this is a much longer text that should be detected as content because it has the most characters on average"},
				{"b", "def", "another long text here for the long column that should trigger detection"},
			},
			expected: []string{"long"},
		},
		{
			name:       "empty sample rows returns nil",
			headers:    []string{"a", "b", "c"},
			sampleRows: [][]string{},
			expected:   nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			detector := NewColumnDetector(tc.headers, tc.sampleRows)
			result := detector.DetectContentColumns()

			if len(tc.expected) == 0 && len(result) == 0 {
				return // Both nil/empty is OK
			}

			if len(result) != len(tc.expected) {
				t.Errorf("expected %v columns, got %v", tc.expected, result)
				return
			}

			for i, col := range result {
				if col != tc.expected[i] {
					t.Errorf("expected column %d to be '%s', got '%s'", i, tc.expected[i], col)
				}
			}
		})
	}
}

func TestColumnDetector_DetectTitleColumn(t *testing.T) {
	tests := []struct {
		name     string
		headers  []string
		expected string
	}{
		{
			name:     "Japanese title",
			headers:  []string{"ID", "タイトル", "本文"},
			expected: "タイトル",
		},
		{
			name:     "English title",
			headers:  []string{"id", "title", "content"},
			expected: "title",
		},
		{
			name:     "subject as title",
			headers:  []string{"id", "subject", "body"},
			expected: "subject",
		},
		{
			name:     "no title column",
			headers:  []string{"id", "data", "value"},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			detector := NewColumnDetector(tc.headers, nil)
			result := detector.DetectTitleColumn()

			if result != tc.expected {
				t.Errorf("expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestColumnDetector_DetectCategoryColumn(t *testing.T) {
	tests := []struct {
		name     string
		headers  []string
		expected string
	}{
		{
			name:     "Japanese category",
			headers:  []string{"ID", "タイトル", "カテゴリ"},
			expected: "カテゴリ",
		},
		{
			name:     "English category",
			headers:  []string{"id", "title", "category"},
			expected: "category",
		},
		{
			name:     "type as category",
			headers:  []string{"id", "title", "type"},
			expected: "type",
		},
		{
			name:     "no category column",
			headers:  []string{"id", "title", "content"},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			detector := NewColumnDetector(tc.headers, nil)
			result := detector.DetectCategoryColumn()

			if result != tc.expected {
				t.Errorf("expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestColumnDetector_DetectIDColumn(t *testing.T) {
	tests := []struct {
		name     string
		headers  []string
		expected string
	}{
		{
			name:     "ID column",
			headers:  []string{"ID", "title", "content"},
			expected: "ID",
		},
		{
			name:     "lowercase id",
			headers:  []string{"id", "title", "content"},
			expected: "id",
		},
		{
			name:     "hash symbol",
			headers:  []string{"#", "title", "content"},
			expected: "#",
		},
		{
			name:     "番号",
			headers:  []string{"番号", "タイトル", "内容"},
			expected: "番号",
		},
		{
			name:     "no ID column",
			headers:  []string{"title", "content", "category"},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			detector := NewColumnDetector(tc.headers, nil)
			result := detector.DetectIDColumn()

			if result != tc.expected {
				t.Errorf("expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestColumnDetector_GetColumnIndex(t *testing.T) {
	headers := []string{"ID", "Title", "Content"}
	detector := NewColumnDetector(headers, nil)

	tests := []struct {
		name     string
		column   string
		expected int
	}{
		{"exact match", "ID", 0},
		{"case insensitive", "title", 1},
		{"not found", "Category", -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := detector.GetColumnIndex(tc.column)
			if result != tc.expected {
				t.Errorf("expected %d, got %d", tc.expected, result)
			}
		})
	}
}

func TestColumnDetector_ColumnExists(t *testing.T) {
	headers := []string{"ID", "Title", "Content"}
	detector := NewColumnDetector(headers, nil)

	if !detector.ColumnExists("ID") {
		t.Error("expected ID to exist")
	}

	if !detector.ColumnExists("title") { // case insensitive
		t.Error("expected title to exist (case insensitive)")
	}

	if detector.ColumnExists("Category") {
		t.Error("expected Category to not exist")
	}
}

func TestMatchesAny(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		knownNames []string
		expected   bool
	}{
		{
			name:       "exact match",
			header:     "content",
			knownNames: []string{"content", "body", "text"},
			expected:   true,
		},
		{
			name:       "case insensitive",
			header:     "CONTENT",
			knownNames: []string{"content", "body", "text"},
			expected:   true,
		},
		{
			name:       "with whitespace",
			header:     "  content  ",
			knownNames: []string{"content", "body", "text"},
			expected:   true,
		},
		{
			name:       "no match",
			header:     "data",
			knownNames: []string{"content", "body", "text"},
			expected:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := matchesAny(tc.header, tc.knownNames)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}
