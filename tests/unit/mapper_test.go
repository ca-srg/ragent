package unit

import (
	"encoding/json"
	"testing"

	"github.com/ca-srg/mdrag/internal/slackbot"
)

func TestMapDocSourceToItem(t *testing.T) {
	src := map[string]interface{}{
		"title":           "設計方針",
		"content_excerpt": "本ドキュメントは…",
		"reference":       "https://example.com/doc",
		"file_path":       "docs/design.md",
		"category":        "architecture",
	}
	b, _ := json.Marshal(src)
	item := slackbot.MapDocSourceToItem(b, "DOCID", "mdrag", 0.87)
	if item.Title != "設計方針" || item.Link != "https://example.com/doc" || item.Snippet == "" {
		t.Fatalf("unexpected mapping: %+v", item)
	}
	if item.Category != "architecture" || item.FilePath != "docs/design.md" {
		t.Fatalf("category/path not mapped: %+v", item)
	}
}
