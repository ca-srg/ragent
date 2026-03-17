package opensearch

import "testing"

func TestBuildTermQueryBodyAppliesSecretExclusion(t *testing.T) {
	query := &TermQuery{
		Field:         "reference",
		Values:        []string{"https://example.com"},
		Size:          10,
		From:          0,
		ExcludeSecret: true,
	}

	body := BuildTermQueryBody(query)
	querySection, ok := body["query"].(map[string]interface{})
	if !ok {
		t.Fatalf("query section missing")
	}

	boolQuery, ok := querySection["bool"].(map[string]interface{})
	if !ok {
		t.Fatalf("bool query missing")
	}

	if _, ok := boolQuery["must_not"]; !ok {
		t.Fatalf("expected must_not clause")
	}
}

func TestBuildTermQueryBodyDoesNotAddSecretClauseWhenDisabled(t *testing.T) {
	query := &TermQuery{
		Field:         "reference",
		Values:        []string{"https://example.com"},
		Size:          10,
		From:          0,
		ExcludeSecret: false,
	}

	body := BuildTermQueryBody(query)
	querySection, ok := body["query"].(map[string]interface{})
	if !ok {
		t.Fatalf("query section missing")
	}

	if _, ok := querySection["bool"]; ok {
		t.Fatalf("expected non-bool query when exclusion disabled")
	}
	if querySection["terms"] == nil {
		t.Fatalf("expected terms query")
	}
}
