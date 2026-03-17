package opensearch

func applySecretExclusion(boolQuery map[string]interface{}, excludeSecret bool) {
	if !excludeSecret || boolQuery == nil {
		return
	}

	secretClause := map[string]interface{}{
		"term": map[string]interface{}{
			"secret": true,
		},
	}

	boolQuery["must_not"] = []map[string]interface{}{secretClause}
}
