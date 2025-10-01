package opensearch

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestBuildBM25QueryEnforcesExactMatchAndLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Default()
	originalWriter := logger.Writer()
	logger.SetOutput(&buf)
	defer logger.SetOutput(originalWriter)

	hse := &HybridSearchEngine{
		textProcessor: NewJapaneseTextProcessor(),
	}

	hybridQuery := &HybridQuery{
		Query:  "0円チャージ",
		Fields: []string{"title"},
		K:      10,
	}

	bm25Query := hse.buildBM25Query(hybridQuery, nil)

	if bm25Query.MinimumShouldMatch != "100%" {
		t.Fatalf("expected minimum_should_match to be 100%%, got %s", bm25Query.MinimumShouldMatch)
	}

	if len(bm25Query.BoostPhrases) != 1 || bm25Query.BoostPhrases[0] != "0円チャージ" {
		t.Fatalf("expected boost phrases [0円チャージ], got %v", bm25Query.BoostPhrases)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "minimum_should_match=100%") {
		t.Fatalf("expected log to contain enforcement message, got %q", logOutput)
	}
	if !strings.Contains(logOutput, "0円チャージ") {
		t.Fatalf("expected log to include original query, got %q", logOutput)
	}
	if !strings.Contains(logOutput, "Adding boosted phrase clauses") {
		t.Fatalf("expected log to mention boosted phrase clauses, got %q", logOutput)
	}
}

func TestBuildBM25QueryDoesNotEnforceForLongQuery(t *testing.T) {
	var buf bytes.Buffer
	logger := log.Default()
	originalWriter := logger.Writer()
	logger.SetOutput(&buf)
	defer logger.SetOutput(originalWriter)

	hse := &HybridSearchEngine{
		textProcessor: NewJapaneseTextProcessor(),
	}

	hybridQuery := &HybridQuery{
		Query:  "チャージに関するキャンペーンの仕様を確認したい",
		Fields: []string{"content"},
		K:      10,
	}

	bm25Query := hse.buildBM25Query(hybridQuery, nil)

	if bm25Query.MinimumShouldMatch != "" {
		t.Fatalf("expected minimum_should_match to remain empty, got %s", bm25Query.MinimumShouldMatch)
	}

	if len(bm25Query.BoostPhrases) != 0 {
		t.Fatalf("expected no boost phrases, got %v", bm25Query.BoostPhrases)
	}

	if buf.Len() != 0 {
		t.Fatalf("expected no enforcement log, got %q", buf.String())
	}
}

func TestBuildBM25QueryExtractsBoostPhraseFromLongQuery(t *testing.T) {
	hse := &HybridSearchEngine{
		textProcessor: NewJapaneseTextProcessor(),
	}

	hybridQuery := &HybridQuery{
		Query:  "0円チャージに関する記事をピックアップしてください",
		Fields: []string{"content", "title"},
		K:      10,
	}

	bm25Query := hse.buildBM25Query(hybridQuery, nil)

	if bm25Query.MinimumShouldMatch != "" {
		t.Fatalf("expected minimum_should_match to stay empty for long query, got %s", bm25Query.MinimumShouldMatch)
	}

	if len(bm25Query.BoostPhrases) != 1 || bm25Query.BoostPhrases[0] != "0円チャージ" {
		t.Fatalf("expected boost phrases [0円チャージ], got %v", bm25Query.BoostPhrases)
	}
}
