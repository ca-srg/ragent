package slackvectorizer

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/slackmessages"
	"github.com/ca-srg/ragent/internal/vectorizer"
	"github.com/ca-srg/ragent/internal/vectorizer/mocks"
	"github.com/stretchr/testify/assert"
)

func TestSlackVectorizerService_VectorizeMessages_Success(t *testing.T) {
	ctx := context.Background()

	embedding := &stubEmbeddingClient{
		vector: makeVector(384),
	}
	s3 := &stubS3Client{}
	osMock := mocks.NewOpenSearchIndexerMock()
	assert.NoError(t, osMock.CreateIndex(ctx, "slack-index", 3))

	fetcher := &stubFetcher{
		messages: []slackmessages.SlackMessage{
			newSlackMessage("C1", "general", "U1", "hello world", "1700000000.000001"),
			newSlackMessage("C2", "random", "U2", "another message", "1700000001.000001"),
		},
	}

	service, err := NewSlackVectorizerService(ServiceConfig{
		EmbeddingClient:       embedding,
		S3Client:              s3,
		OpenSearchIndexer:     osMock,
		MessageFetcher:        fetcher,
		Logger:                log.New(io.Discard, "", 0),
		EnableOpenSearch:      true,
		OpenSearchIndexName:   "slack-index",
		Concurrency:           2,
		MinMessageLength:      3,
		RetryAttempts:         2,
		RetryDelay:            time.Millisecond,
		UseJapaneseProcessing: true,
	})
	assert.NoError(t, err)

	stats, err := service.VectorizeMessages(ctx, slackmessages.FetchConfig{}, false)
	if err != nil {
		t.Logf("stats errors: %+v", stats.Errors)
	}
	assert.NoError(t, err)
	assert.Equal(t, 2, stats.MessagesProcessed)
	assert.Zero(t, stats.MessagesFailed)
	assert.Equal(t, 2, len(s3.vectors))
	assert.Equal(t, 2, osMock.GetIndexedDocumentCount())
}

func TestSlackVectorizerService_VectorizeMessages_RetryAndFailure(t *testing.T) {
	ctx := context.Background()

	embedding := &stubEmbeddingClient{vector: makeVector(384)}
	s3 := &stubS3Client{failuresRemaining: 1}
	osMock := mocks.NewOpenSearchIndexerMock()
	assert.NoError(t, osMock.CreateIndex(ctx, "slack-index", 1))

	fetcher := &stubFetcher{
		messages: []slackmessages.SlackMessage{
			newSlackMessage("C1", "general", "U1", "retry me", "1700000010.000001"),
		},
	}

	service, err := NewSlackVectorizerService(ServiceConfig{
		EmbeddingClient:     embedding,
		S3Client:            s3,
		OpenSearchIndexer:   osMock,
		MessageFetcher:      fetcher,
		Logger:              log.New(io.Discard, "", 0),
		EnableOpenSearch:    true,
		OpenSearchIndexName: "slack-index",
		RetryAttempts:       3,
		RetryDelay:          time.Millisecond,
	})
	assert.NoError(t, err)

	stats, err := service.VectorizeMessages(ctx, slackmessages.FetchConfig{}, false)
	if err != nil {
		t.Logf("stats errors: %+v", stats.Errors)
	}
	assert.NoError(t, err)
	assert.Equal(t, 1, stats.MessagesProcessed)
	assert.Equal(t, 1, stats.Retries)
}

func TestSlackVectorizerService_VectorizeRealtime_ShortMessage(t *testing.T) {
	service, err := NewSlackVectorizerService(ServiceConfig{
		EmbeddingClient:  &stubEmbeddingClient{vector: []float64{0.4}},
		S3Client:         &stubS3Client{},
		MessageFetcher:   &stubFetcher{},
		Logger:           log.New(io.Discard, "", 0),
		Concurrency:      1,
		MinMessageLength: 10,
	})
	assert.NoError(t, err)

	err = service.VectorizeRealtime(context.Background(), newSlackMessage("C1", "general", "U1", "short", "1700000500.000001"), false)
	assert.NoError(t, err)
}

// --- Test helpers ---

type stubEmbeddingClient struct {
	mu     sync.Mutex
	vector []float64
	err    error
	calls  int
}

func (s *stubEmbeddingClient) GenerateEmbedding(ctx context.Context, text string) ([]float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return append([]float64(nil), s.vector...), nil
}

func (s *stubEmbeddingClient) ValidateConnection(ctx context.Context) error { return nil }
func (s *stubEmbeddingClient) GetModelInfo() (string, int, error)           { return "stub", len(s.vector), nil }

type stubS3Client struct {
	mu                sync.Mutex
	vectors           map[string]*vectorizer.VectorData
	err               error
	failuresRemaining int
}

func (s *stubS3Client) StoreVector(ctx context.Context, vectorData *vectorizer.VectorData) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failuresRemaining > 0 {
		s.failuresRemaining--
		return errors.New("temporary s3 failure")
	}

	if s.err != nil {
		return s.err
	}

	if s.vectors == nil {
		s.vectors = make(map[string]*vectorizer.VectorData)
	}
	s.vectors[vectorData.ID] = vectorData
	return nil
}

func (s *stubS3Client) ValidateAccess(ctx context.Context) error { return nil }
func (s *stubS3Client) ListVectors(ctx context.Context, prefix string) ([]string, error) {
	return nil, nil
}
func (s *stubS3Client) DeleteVector(ctx context.Context, vectorID string) error { return nil }
func (s *stubS3Client) GetBucketInfo(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

type stubFetcher struct {
	messages []slackmessages.SlackMessage
	err      error
}

func (s *stubFetcher) FetchMessages(ctx context.Context, cfg slackmessages.FetchConfig) ([]slackmessages.SlackMessage, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]slackmessages.SlackMessage(nil), s.messages...), nil
}

func newSlackMessage(channelID, channelName, userID, text, ts string) slackmessages.SlackMessage {
	return slackmessages.SlackMessage{
		ChannelID:   channelID,
		ChannelName: channelName,
		UserID:      userID,
		UserName:    userID,
		Text:        text,
		Timestamp:   ts,
	}
}

func makeVector(dim int) []float64 {
	vec := make([]float64, dim)
	for i := range vec {
		vec[i] = float64(i%10) * 0.01
	}
	return vec
}
