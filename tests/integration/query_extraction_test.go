package integration

import (
	"testing"

	"github.com/ca-srg/ragent/internal/slackbot"
)

func TestExtractorRemovesMentionAndTrims(t *testing.T) {
	e := &slackbot.QueryExtractor{}
	got := e.ExtractQuery("UBOT", "  <@UBOT>   RAG の流れ  ")
	if got != "RAG の流れ" {
		t.Fatalf("unexpected extracted query: %q", got)
	}
}
