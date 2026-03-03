package observability

import (
	"fmt"
	"net/url"
	"strings"
)

// normalizeOTLPHTTPPath ensures that the given OTLP HTTP endpoint includes the expected signal suffix.
// When the endpoint already ends with the suffix (e.g. /v1/metrics) it is returned unchanged.
// If the endpoint has no path component, the suffix is appended.
// The function never removes existing query parameters or fragments.
func normalizeOTLPHTTPPath(endpoint string, suffix string) (string, error) {
	if strings.TrimSpace(endpoint) == "" {
		return "", fmt.Errorf("endpoint cannot be empty")
	}

	normalizedSuffix := "/" + strings.Trim(strings.TrimSpace(suffix), "/")

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}

	trimmedPath := strings.TrimSuffix(parsed.Path, "/")
	switch {
	case trimmedPath == "":
		parsed.Path = normalizedSuffix
	case strings.HasSuffix(trimmedPath, normalizedSuffix):
		parsed.Path = trimmedPath
	default:
		parsed.Path = trimmedPath + normalizedSuffix
	}

	return parsed.String(), nil
}
