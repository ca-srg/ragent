package slackbot

import (
	"encoding/json"

	"github.com/ca-srg/mdrag/internal/opensearch"
)

// osSource mirrors common fields present in OpenSearch _source
type osSource struct {
	Title          string `json:"title"`
	Content        string `json:"content"`
	ContentExcerpt string `json:"content_excerpt"`
	Reference      string `json:"reference"`
	FilePath       string `json:"file_path"`
	Category       string `json:"category"`
}

// MapDocSourceToItem converts a single OpenSearch _source JSON into SearchItem
func MapDocSourceToItem(src json.RawMessage, id, index string, score float64) SearchItem {
	var s osSource
	_ = json.Unmarshal(src, &s)
	item := SearchItem{
		Title:    s.Title,
		Snippet:  s.ContentExcerpt,
		Score:    score,
		Source:   index,
		Link:     s.Reference,
		Category: s.Category,
		FilePath: s.FilePath,
	}
	if item.Title == "" {
		item.Title = id
	}
	if item.Snippet == "" && s.Content != "" {
		if len(s.Content) > 140 {
			item.Snippet = s.Content[:140] + "..."
		} else {
			item.Snippet = s.Content
		}
	}
	return item
}

// MapFusionToItems converts a FusionResult into SearchItems
func MapFusionToItems(fr *opensearch.FusionResult) []SearchItem {
	if fr == nil || len(fr.Documents) == 0 {
		return nil
	}
	items := make([]SearchItem, 0, len(fr.Documents))
	for _, d := range fr.Documents {
		items = append(items, MapDocSourceToItem(d.Source, d.ID, d.Index, d.FusedScore))
	}
	return items
}
