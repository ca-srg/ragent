package mcpserver

import "github.com/ca-srg/ragent/internal/pkg/mcpclient"

func attachMCPResultsToHybridResponse(response *HybridSearchResponse, result *mcpclient.QueryResult) {
	if response == nil || result == nil || len(result.Results) == 0 {
		return
	}
	response.MCPResults = result.Results
	response.Total += len(result.Results)
	response.SearchSources = appendSearchSource(response.SearchSources, "mcp")
}

func attachMCPResultsToSlackResponse(response *SlackSearchResponse, result *mcpclient.QueryResult) {
	if response == nil || result == nil || len(result.Results) == 0 {
		return
	}
	response.MCPResults = result.Results
	response.Total += len(result.Results)
}

func appendSearchSource(sources []string, source string) []string {
	for _, existing := range sources {
		if existing == source {
			return sources
		}
	}
	return append(sources, source)
}
