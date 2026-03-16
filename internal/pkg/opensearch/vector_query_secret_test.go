package opensearch

import "testing"

func TestBuildVectorSearchBodyAppliesSecretExclusion(t *testing.T) {
	client := &Client{}
	query := &VectorQuery{
		Vector:        []float64{0.1, 0.2, 0.3},
		VectorField:   "embedding",
		K:             5,
		Size:          3,
		From:          0,
		ExcludeSecret: true,
	}

	body := client.buildVectorSearchBody(query)
	querySection, ok := body["query"].(map[string]interface{})
	if !ok {
		t.Fatalf("query section missing")
	}

	boolQuery, ok := querySection["bool"].(map[string]interface{})
	if !ok {
		t.Fatalf("bool query missing")
	}

	mustNot, ok := boolQuery["must_not"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected must_not clause")
	}
	if len(mustNot) != 1 {
		t.Fatalf("expected one must_not clause, got %d", len(mustNot))
	}
}

func TestBuildVectorSearchBodyDoesNotAddSecretClauseWhenDisabled(t *testing.T) {
	client := &Client{}
	query := &VectorQuery{
		Vector:        []float64{0.1, 0.2, 0.3},
		VectorField:   "embedding",
		K:             5,
		Size:          3,
		ExcludeSecret: false,
	}

	body := client.buildVectorSearchBody(query)
	querySection, ok := body["query"].(map[string]interface{})
	if !ok {
		t.Fatalf("query section missing")
	}

	if _, ok := querySection["bool"]; ok {
		t.Fatalf("did not expect bool query when secret exclusion is disabled")
	}
}
