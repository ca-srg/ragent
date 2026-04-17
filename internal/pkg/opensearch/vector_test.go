package opensearch

import "testing"

func TestBuildVectorSearchBodyExcludesEmbedding(t *testing.T) {
	client := &Client{}
	query := &VectorQuery{
		VectorField: "embedding",
		Vector:      []float64{0.1, 0.2, 0.3},
		K:           5,
		Size:        3,
	}

	body := client.buildVectorSearchBody(query)
	source, ok := body["_source"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected _source section")
	}

	excludes, ok := source["excludes"].([]string)
	if !ok {
		t.Fatalf("expected excludes to be []string")
	}

	if len(excludes) != 1 || excludes[0] != "embedding" {
		t.Fatalf("expected excludes to contain embedding, got %#v", excludes)
	}
}
