package opensearch

import (
	"regexp"
	"strings"
)

// URLDetectorはクエリ文字列からHTTP/HTTPS URLを抽出するインターフェース。
type URLDetector interface {
	DetectURLs(query string) *URLDetectionResult
}

// URLDetectionResultは検出結果を表す。
type URLDetectionResult struct {
	HasURL        bool
	URLs          []string
	OriginalQuery string
}

type defaultURLDetector struct {
	urlPattern *regexp.Regexp
}

var defaultURLPattern = regexp.MustCompile(`(?i)https?://[^\s\p{Zs}]+`)

// NewURLDetectorはデフォルト実装を返す。
func NewURLDetector() URLDetector {
	return &defaultURLDetector{urlPattern: defaultURLPattern}
}

// DetectURLsはクエリからURLを検出して結果を返す。
func (d *defaultURLDetector) DetectURLs(query string) *URLDetectionResult {
	result := &URLDetectionResult{OriginalQuery: query}

	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return result
	}

	matches := d.urlPattern.FindAllString(trimmed, -1)
	if len(matches) == 0 {
		return result
	}

	seen := make(map[string]struct{}, len(matches))
	for _, url := range matches {
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		result.URLs = append(result.URLs, url)
	}

	if len(result.URLs) > 0 {
		result.HasURL = true
	}

	return result
}
