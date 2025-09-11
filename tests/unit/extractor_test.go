package unit

import (
	"testing"

	"github.com/ca-srg/mdrag/internal/slackbot"
)

func TestQueryExtractor(t *testing.T) {
	e := &slackbot.QueryExtractor{}
	botID := "U999"
	q := e.ExtractQuery(botID, "<@U999> AWS のRAG構成教えて")
	if q != "AWS のRAG構成教えて" {
		t.Fatalf("unexpected query: %q", q)
	}

	q2 := e.ExtractQuery(botID, "")
	if q2 != "" {
		t.Fatalf("expected empty query")
	}
}
