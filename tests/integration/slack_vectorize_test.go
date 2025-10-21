//go:build integration
// +build integration

package integration

import (
	"context"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/slackbot"
	"github.com/ca-srg/ragent/internal/slackmessages"
	"github.com/ca-srg/ragent/internal/slackvectorizer"
	"github.com/ca-srg/ragent/internal/vectorizer"
	"github.com/ca-srg/ragent/internal/vectorizer/mocks"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
)

func TestSlackVectorizationFlow_BatchAndRealtime(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("set INTEGRATION_TEST=true to run Slack vectorization integration tests")
	}

	ctx := context.Background()
	embedding := &integrationEmbeddingClient{vector: makeIntegrationVector(384)}
	s3 := newIntegrationS3Client()
	openSearch := mocks.NewOpenSearchIndexerMock()
	require.NoError(t, openSearch.CreateIndex(ctx, "slack-integration", 3))

	fetcher := &integrationFetcher{
		messages: []slackmessages.SlackMessage{
			{
				ChannelID:       "C123",
				ChannelName:     "general",
				UserID:          "U111",
				UserName:        "bob",
				Text:            "Incident report 2025-10-20",
				Timestamp:       "1700000000.000001",
				ThreadTimestamp: "1700000000.000001",
				Permalink:       "https://slack.example.com/archives/C123/p1700000000000001",
			},
			{
				ChannelID:       "C456",
				ChannelName:     "dev",
				UserID:          "U222",
				UserName:        "sara",
				Text:            "Release checklist draft",
				Timestamp:       "1700000100.000001",
				ThreadTimestamp: "1700000100.000001",
				Permalink:       "https://slack.example.com/archives/C456/p1700000100000001",
			},
		},
	}

	service, err := slackvectorizer.NewSlackVectorizerService(slackvectorizer.ServiceConfig{
		EmbeddingClient:       embedding,
		S3Client:              s3,
		OpenSearchIndexer:     openSearch,
		MessageFetcher:        fetcher,
		Logger:                log.New(io.Discard, "", 0),
		EnableOpenSearch:      true,
		OpenSearchIndexName:   "slack-integration",
		UseJapaneseProcessing: true,
		Concurrency:           2,
		MinMessageLength:      1,
		RetryAttempts:         1,
		RetryDelay:            10 * time.Millisecond,
	})
	require.NoError(t, err)

	// Batch vectorization path via fetcher
	stats, err := service.VectorizeMessages(ctx, slackmessages.FetchConfig{IncludeThreads: true}, false)
	if err != nil {
		t.Fatalf("batch vectorization failed: %v (errors=%v)", err, stats.Errors)
	}
	require.Equal(t, 2, stats.MessagesProcessed)
	s3.waitForCount(t, 2, time.Second)

	require.Len(t, openSearch.Documents["slack-integration"], 2)
	require.True(t, containsDocumentWithText(openSearch.Documents["slack-integration"], "Incident report"))

	// Simulate realtime ingestion directly through service
	realtimeMessage := slackmessages.SlackMessage{
		ChannelID:       "C789",
		ChannelName:     "ops",
		UserID:          "U333",
		UserName:        "mika",
		Text:            "Realtime status update for deployment",
		Timestamp:       "1700005000.000001",
		ThreadTimestamp: "1700005000.000001",
		Permalink:       "https://slack.example.com/archives/C789/p1700005000000001",
	}
	require.NoError(t, service.VectorizeRealtime(ctx, realtimeMessage, false))
	s3.waitForCount(t, 3, time.Second)
	require.Len(t, openSearch.Documents["slack-integration"], 3)
	require.True(t, containsDocumentWithText(openSearch.Documents["slack-integration"], "Realtime status update"))

	// Realtime ingestion triggered through Slack Bot event loop
	slackClient := &integrationSlackClient{}
	rtm := newIntegrationRTM()
	searchAdapter := &noopSearchAdapter{}
	processor := slackbot.NewProcessor(&slackbot.MentionDetector{}, &slackbot.QueryExtractor{}, searchAdapter, &slackbot.Formatter{}, nil)
	bot := slackbot.NewBotWithRTM(slackClient, rtm, processor, log.New(io.Discard, "", 0), "UBOT")
	bot.SetVectorizer(service, slackbot.VectorizeOptions{
		Enabled:          true,
		Channels:         []string{"C-live"},
		ExcludeBots:      true,
		MinMessageLength: 1,
	})

	botCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = bot.Start(botCtx)
		close(done)
	}()

	rtm.Emit(slack.RTMEvent{
		Data: &slack.MessageEvent{
			Msg: slack.Msg{
				Channel:   "C-live",
				User:      "U-live",
				Text:      "Realtime playbook update from bot flow",
				Timestamp: "1700007000.000001",
			},
		},
	})

	s3.waitForCount(t, 4, 2*time.Second)
	require.Len(t, openSearch.Documents["slack-integration"], 4)
	require.True(t, containsDocumentWithText(openSearch.Documents["slack-integration"], "playbook update"))

	cancel()
	<-done
}

// --- Helpers ---

type integrationEmbeddingClient struct {
	mu     sync.Mutex
	vector []float64
}

func (c *integrationEmbeddingClient) GenerateEmbedding(ctx context.Context, text string) ([]float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]float64(nil), c.vector...), nil
}

func (c *integrationEmbeddingClient) ValidateConnection(ctx context.Context) error { return nil }
func (c *integrationEmbeddingClient) GetModelInfo() (string, int, error) {
	return "stub", len(c.vector), nil
}

type integrationFetcher struct {
	messages []slackmessages.SlackMessage
}

func (f *integrationFetcher) FetchMessages(ctx context.Context, cfg slackmessages.FetchConfig) ([]slackmessages.SlackMessage, error) {
	return append([]slackmessages.SlackMessage(nil), f.messages...), nil
}

type integrationS3Client struct {
	mu      sync.Mutex
	vectors map[string]*vectorizer.VectorData
	notify  chan struct{}
}

func newIntegrationS3Client() *integrationS3Client {
	return &integrationS3Client{
		vectors: make(map[string]*vectorizer.VectorData),
		notify:  make(chan struct{}, 16),
	}
}

func (s *integrationS3Client) StoreVector(ctx context.Context, vectorData *vectorizer.VectorData) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.vectors[vectorData.ID] = vectorData
	select {
	case s.notify <- struct{}{}:
	default:
	}
	return nil
}

func (s *integrationS3Client) ValidateAccess(ctx context.Context) error { return nil }
func (s *integrationS3Client) ListVectors(ctx context.Context, prefix string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.vectors))
	for id := range s.vectors {
		ids = append(ids, id)
	}
	return ids, nil
}
func (s *integrationS3Client) DeleteVector(ctx context.Context, vectorID string) error { return nil }
func (s *integrationS3Client) GetBucketInfo(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (s *integrationS3Client) waitForCount(t *testing.T, expected int, timeout time.Duration) {
	deadline := time.After(timeout)
	for {
		s.mu.Lock()
		count := len(s.vectors)
		s.mu.Unlock()
		if count >= expected {
			return
		}
		select {
		case <-s.notify:
		case <-deadline:
			t.Fatalf("timeout waiting for %d vectors (have %d)", expected, count)
		}
	}
}

type integrationSlackClient struct{}

func (c *integrationSlackClient) AuthTest() (*slack.AuthTestResponse, error) {
	return &slack.AuthTestResponse{UserID: "UBOT"}, nil
}
func (c *integrationSlackClient) NewRTM(options ...slack.RTMOption) *slack.RTM { return nil }
func (c *integrationSlackClient) PostMessage(channelID string, options ...slack.MsgOption) (string, string, error) {
	return "", "", nil
}
func (c *integrationSlackClient) GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
	return nil, false, "", nil
}
func (c *integrationSlackClient) GetConversationInfo(input *slack.GetConversationInfoInput) (*slack.Channel, error) {
	return &slack.Channel{}, nil
}
func (c *integrationSlackClient) GetUserInfo(userID string) (*slack.User, error) {
	return &slack.User{}, nil
}
func (c *integrationSlackClient) GetPermalink(params *slack.PermalinkParameters) (string, error) {
	return "", nil
}

type integrationRTM struct {
	events chan slack.RTMEvent
}

func newIntegrationRTM() *integrationRTM {
	return &integrationRTM{events: make(chan slack.RTMEvent, 8)}
}

func (r *integrationRTM) ManageConnection() {}
func (r *integrationRTM) IncomingEvents() chan slack.RTMEvent {
	return r.events
}
func (r *integrationRTM) Disconnect() error { close(r.events); return nil }
func (r *integrationRTM) SendMessage(channel string, opts ...slack.MsgOption) (string, string, error) {
	return "", "", nil
}
func (r *integrationRTM) Typing(channel string) {}
func (r *integrationRTM) Emit(event slack.RTMEvent) {
	r.events <- event
}

type noopSearchAdapter struct{}

func (s *noopSearchAdapter) Search(ctx context.Context, query string) *slackbot.SearchResult {
	return nil
}

func containsDocumentWithText(documents map[string]*vectorizer.OpenSearchDocument, text string) bool {
	for _, doc := range documents {
		if strings.Contains(doc.Content, text) {
			return true
		}
	}
	return false
}

func makeIntegrationVector(dim int) []float64 {
	vec := make([]float64, dim)
	for i := range vec {
		vec[i] = float64((i%5)+1) * 0.01
	}
	return vec
}
