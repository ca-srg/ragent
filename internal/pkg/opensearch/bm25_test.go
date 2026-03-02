package opensearch

import "testing"

func TestBuildBM25SearchBodyAddsPhraseShouldClause(t *testing.T) {
	client := &Client{}
	query := &BM25Query{
		Query:  "0円チャージ",
		Fields: []string{"title", "content"},
		Size:   10,
	}

	body := client.buildBM25SearchBody(query)
	querySection, ok := body["query"].(map[string]interface{})
	if !ok {
		t.Fatalf("query section missing")
	}

	boolQuery, ok := querySection["bool"].(map[string]interface{})
	if !ok {
		t.Fatalf("bool query missing")
	}

	shouldClause, ok := boolQuery["should"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected should clause to be added")
	}

	if len(shouldClause) == 0 {
		t.Fatalf("should clause should contain at least one element")
	}

	phraseFound := false
	for _, clause := range shouldClause {
		multiMatch, ok := clause["multi_match"].(map[string]interface{})
		if !ok {
			continue
		}

		typeValue, _ := multiMatch["type"].(string)
		if typeValue != "phrase" {
			continue
		}

		if boost, ok := multiMatch["boost"].(float64); !ok || boost != phraseMatchBoost {
			continue
		}

		if q, _ := multiMatch["query"].(string); q == query.Query {
			phraseFound = true
			break
		}
	}

	if !phraseFound {
		t.Fatalf("expected multi_match phrase clause to be present")
	}
}

func TestBuildMatchPhraseQuerySingleFieldUsesMatchPhrase(t *testing.T) {
	client := &Client{}
	query := &BM25Query{
		Query:  "0円チャージ",
		Fields: []string{"content"},
	}

	clause := client.buildMatchPhraseQuery(query)
	if clause == nil {
		t.Fatalf("expected clause to be generated")
	}

	matchPhrase, ok := clause["match_phrase"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected match_phrase clause")
	}

	fieldClause, ok := matchPhrase[query.Fields[0]].(map[string]interface{})
	if !ok {
		t.Fatalf("expected single field clause")
	}

	if q, _ := fieldClause["query"].(string); q != query.Query {
		t.Fatalf("expected query to match, got %s", q)
	}

	if boost, _ := fieldClause["boost"].(float64); boost != phraseMatchBoost {
		t.Fatalf("expected boost %.1f, got %v", phraseMatchBoost, boost)
	}
}
