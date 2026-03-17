package mcpserver

import (
	"context"
	"testing"
)

func TestHybridSearchTool_parseParamsStripsSecretFilter(t *testing.T) {
	adapter := &HybridSearchToolAdapter{}

	request, err := adapter.parseParams(map[string]interface{}{
		"query": "kiberag",
		"filters": map[string]interface{}{
			"team":   "search",
			"secret": "true",
			"SeCrEt": "false",
		},
	})
	if err != nil {
		t.Fatalf("parseParams returned error: %v", err)
	}

	if request.Filters == nil {
		t.Fatalf("filters should be initialized")
	}
	if _, ok := request.Filters["secret"]; ok {
		t.Fatalf("secret filter should be removed")
	}
	if got := request.Filters["team"]; got != "search" {
		t.Fatalf("expected team filter to be preserved, got %q", got)
	}
}

func TestHybridSearchTool_SecretPolicy_DefaultDenyInContextlessRequest(t *testing.T) {
	adapter := &HybridSearchToolAdapter{}
	request := &HybridSearchRequest{}

	adapter.applySecretPolicyFromContext(context.Background(), request)
	if !request.ExcludeSecret {
		t.Fatalf("expected secret docs to be excluded without OIDC context")
	}
}

func TestHybridSearchTool_SecretPolicy_AllowsOIDCAuthenticatedRequest(t *testing.T) {
	adapter := &HybridSearchToolAdapter{}
	request := &HybridSearchRequest{}
	ctx := context.WithValue(context.Background(), userContextKey, &TokenInfo{Subject: "user-1"})

	adapter.applySecretPolicyFromContext(ctx, request)
	if request.ExcludeSecret {
		t.Fatalf("expected secret docs to be included for OIDC-authenticated request")
	}
}

func TestHybridSearchTool_BuildHybridQueryPropagatesSecretPolicy(t *testing.T) {
	adapter := &HybridSearchToolAdapter{}
	request := &HybridSearchRequest{
		Query:         "kiberag",
		ExcludeSecret: true,
	}

	query := adapter.buildHybridQuery(request)
	if query == nil || !query.ExcludeSecret {
		t.Fatalf("expected exclude_secret=true to be propagated to opensearch query")
	}
}
