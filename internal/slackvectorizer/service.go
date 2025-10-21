package slackvectorizer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ca-srg/ragent/internal/slackmessages"
	"github.com/ca-srg/ragent/internal/types"
	"github.com/ca-srg/ragent/internal/vectorizer"
	"golang.org/x/sync/errgroup"
)

// MessageFetcher defines the contract for retrieving Slack messages.
type MessageFetcher interface {
	FetchMessages(ctx context.Context, cfg slackmessages.FetchConfig) ([]slackmessages.SlackMessage, error)
}

// ServiceConfig contains dependencies and runtime settings for SlackVectorizerService.
type ServiceConfig struct {
	EmbeddingClient       vectorizer.EmbeddingClient
	S3Client              vectorizer.S3VectorClient
	OpenSearchIndexer     vectorizer.OpenSearchIndexer
	MessageFetcher        MessageFetcher
	ErrorHandler          *vectorizer.DualBackendErrorHandler
	Logger                *log.Logger
	EnableOpenSearch      bool
	OpenSearchIndexName   string
	UseJapaneseProcessing bool
	Concurrency           int
	MinMessageLength      int
	RetryAttempts         int
	RetryDelay            time.Duration
}

// SlackVectorizerService vectorizes Slack messages into S3 Vector (and OpenSearch when enabled).
type SlackVectorizerService struct {
	embeddingClient   vectorizer.EmbeddingClient
	s3Client          vectorizer.S3VectorClient
	openSearchIndexer vectorizer.OpenSearchIndexer
	messageFetcher    MessageFetcher
	errorHandler      *vectorizer.DualBackendErrorHandler
	logger            *log.Logger

	enableOpenSearch      bool
	openSearchIndexName   string
	useJapaneseProcessing bool
	concurrency           int
	minMessageLength      int
	retryAttempts         int
	retryDelay            time.Duration
}

// ProcessingStats captures statistics for a vectorization run.
type ProcessingStats struct {
	mu sync.Mutex

	StartTime time.Time
	EndTime   time.Time

	ChannelsProcessed int
	MessagesTotal     int
	MessagesProcessed int
	MessagesFailed    int
	MessagesSkipped   int
	Retries           int

	Errors []types.ProcessingError
}

// NewSlackVectorizerService creates a configured SlackVectorizerService.
func NewSlackVectorizerService(cfg ServiceConfig) (*SlackVectorizerService, error) {
	if cfg.EmbeddingClient == nil {
		return nil, fmt.Errorf("embedding client is required")
	}
	if cfg.S3Client == nil {
		return nil, fmt.Errorf("S3 vector client is required")
	}
	if cfg.MessageFetcher == nil {
		return nil, fmt.Errorf("message fetcher is required")
	}
	if cfg.EnableOpenSearch {
		if cfg.OpenSearchIndexer == nil {
			return nil, fmt.Errorf("open search indexer is required when OpenSearch is enabled")
		}
		if strings.TrimSpace(cfg.OpenSearchIndexName) == "" {
			return nil, fmt.Errorf("open search index name is required when OpenSearch is enabled")
		}
	}

	logger := cfg.Logger
	if logger == nil {
		logger = log.New(os.Stdout, "slack-vectorizer ", log.LstdFlags)
	}

	errorHandler := cfg.ErrorHandler
	if errorHandler == nil {
		errorHandler = vectorizer.NewDualBackendErrorHandler(3, 2*time.Second)
	}

	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}

	retryAttempts := cfg.RetryAttempts
	if retryAttempts <= 0 {
		retryAttempts = 3
	}

	retryDelay := cfg.RetryDelay
	if retryDelay <= 0 {
		retryDelay = time.Second
	}

	statsMin := cfg.MinMessageLength
	if statsMin < 0 {
		statsMin = 0
	}

	return &SlackVectorizerService{
		embeddingClient:       cfg.EmbeddingClient,
		s3Client:              cfg.S3Client,
		openSearchIndexer:     cfg.OpenSearchIndexer,
		messageFetcher:        cfg.MessageFetcher,
		errorHandler:          errorHandler,
		logger:                logger,
		enableOpenSearch:      cfg.EnableOpenSearch,
		openSearchIndexName:   cfg.OpenSearchIndexName,
		useJapaneseProcessing: cfg.UseJapaneseProcessing,
		concurrency:           concurrency,
		minMessageLength:      statsMin,
		retryAttempts:         retryAttempts,
		retryDelay:            retryDelay,
	}, nil
}

// VectorizeMessages performs batch vectorization for messages resolved by the fetcher.
func (s *SlackVectorizerService) VectorizeMessages(ctx context.Context, fetchCfg slackmessages.FetchConfig, dryRun bool) (*ProcessingStats, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	stats := &ProcessingStats{
		StartTime: time.Now(),
		Errors:    make([]types.ProcessingError, 0),
	}
	defer stats.finalize()

	// Align fetch config with service defaults.
	if fetchCfg.MinMessageLength < s.minMessageLength {
		fetchCfg.MinMessageLength = s.minMessageLength
	}

	s.logger.Println("Fetching Slack messages for vectorization...")
	messages, err := s.messageFetcher.FetchMessages(ctx, fetchCfg)
	if err != nil {
		return stats, fmt.Errorf("fetch messages: %w", err)
	}

	if len(messages) == 0 {
		s.logger.Println("No Slack messages to vectorize.")
		return stats, nil
	}

	grouped := groupMessagesByChannel(messages)
	stats.ChannelsProcessed = len(grouped)
	stats.MessagesTotal = len(messages)

	s.logger.Printf("Processing %d messages across %d channels (concurrency=%d)\n",
		len(messages), len(grouped), s.concurrency)

	sem := make(chan struct{}, s.concurrency)
	eg, egCtx := errgroup.WithContext(ctx)

	for channelID, channelMessages := range grouped {
		chID := channelID
		msgs := channelMessages

		sem <- struct{}{}
		eg.Go(func() error {
			defer func() { <-sem }()
			s.processChannel(egCtx, stats, chID, msgs, dryRun)
			return nil
		})
	}

	_ = eg.Wait() // errors are tracked inside stats

	if stats.MessagesFailed > 0 {
		return stats, fmt.Errorf("vectorization completed with %d failures", stats.MessagesFailed)
	}
	return stats, nil
}

// VectorizeRealtime processes a single Slack message (used by realtime integrations).
func (s *SlackVectorizerService) VectorizeRealtime(ctx context.Context, message slackmessages.SlackMessage, dryRun bool) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if s.shouldSkipMessage(message) {
		return nil
	}

	success, _, errs := s.vectorizeMessage(ctx, message, dryRun)
	if success {
		return nil
	}

	if len(errs) == 0 {
		return fmt.Errorf("vectorization failed for message %s", message.GenerateVectorID())
	}

	s.logErrors(errs)
	return fmt.Errorf("vectorization failed for message %s: %s", message.GenerateVectorID(), errs[0].Message)
}

func (s *SlackVectorizerService) processChannel(ctx context.Context, stats *ProcessingStats, channelID string, messages []slackmessages.SlackMessage, dryRun bool) {
	for _, m := range messages {
		select {
		case <-ctx.Done():
			stats.incrementFailed()
			stats.appendError(vectorizer.WrapError(ctx.Err(), vectorizer.ErrorTypeTimeout, channelID))
			return
		default:
		}

		if s.shouldSkipMessage(m) {
			stats.incrementSkipped()
			continue
		}

		success, retries, errs := s.vectorizeMessage(ctx, m, dryRun)
		stats.addRetries(retries)

		if success {
			stats.incrementProcessed()
			continue
		}

		stats.incrementFailed()
		for _, e := range errs {
			stats.appendError(e)
		}
	}
}

func (s *SlackVectorizerService) vectorizeMessage(ctx context.Context, message slackmessages.SlackMessage, dryRun bool) (bool, int, []*types.ProcessingError) {
	if ctx.Err() != nil {
		return false, 0, []*types.ProcessingError{
			vectorizer.WrapError(ctx.Err(), vectorizer.ErrorTypeTimeout, message.GenerateVectorID()),
		}
	}

	if dryRun {
		return true, 0, nil
	}

	embedding, err := s.embeddingClient.GenerateEmbedding(ctx, message.Text)
	if err != nil {
		return false, 0, []*types.ProcessingError{
			vectorizer.WrapError(err, vectorizer.ErrorTypeEmbedding, message.GenerateVectorID()),
		}
	}

	if len(embedding) == 0 {
		return false, 0, []*types.ProcessingError{
			vectorizer.NewProcessingError(vectorizer.ErrorTypeEmbedding, "empty embedding generated", message.GenerateVectorID()),
		}
	}

	vectorData := message.ToVectorData(embedding)
	vectorDataPtr := (*vectorizer.VectorData)(&vectorData)

	var lastErrors []*types.ProcessingError
	retries := 0

	for attempt := 1; attempt <= s.retryAttempts; attempt++ {
		s3Err, osErr := s.storeVector(ctx, vectorDataPtr)

		if s3Err == nil && osErr == nil {
			return true, retries, nil
		}

		lastErrors = lastErrors[:0]
		if s3Err != nil {
			lastErrors = append(lastErrors, vectorizer.WrapError(s3Err, vectorizer.ErrorTypeS3Upload, vectorData.Metadata.FilePath))
		}
		if osErr != nil {
			lastErrors = append(lastErrors, vectorizer.WrapError(osErr, vectorizer.ErrorTypeOpenSearchIndexing, vectorData.Metadata.FilePath))
		}

		shouldRetry := false
		for _, pe := range lastErrors {
			if pe != nil && pe.IsRetryable() {
				shouldRetry = true
				break
			}
		}

		if !shouldRetry || attempt == s.retryAttempts {
			break
		}

		retries++
		time.Sleep(s.retryDelay * time.Duration(attempt))
	}

	return false, retries, lastErrors
}

func (s *SlackVectorizerService) storeVector(ctx context.Context, vectorData *vectorizer.VectorData) (error, error) {
	if ctx.Err() != nil {
		return ctx.Err(), nil
	}

	var s3Err error
	if s.s3Client != nil {
		s3Err = s.s3Client.StoreVector(ctx, vectorData)
	}

	var osErr error
	if s.enableOpenSearch {
		if s.openSearchIndexer == nil {
			osErr = errors.New("open search indexer is not configured")
		} else {
			contentJa := ""
			if s.useJapaneseProcessing {
				processed, err := s.openSearchIndexer.ProcessJapaneseText(vectorData.Content)
				if err != nil {
					osErr = err
				} else {
					contentJa = processed
				}
			}

			if osErr == nil {
				doc := vectorizer.NewOpenSearchDocument(vectorData, contentJa)
				if err := doc.Validate(); err != nil {
					osErr = err
				} else {
					osErr = s.openSearchIndexer.IndexDocument(ctx, s.openSearchIndexName, doc)
				}
			}
		}
	}

	return s3Err, osErr
}

func (s *SlackVectorizerService) shouldSkipMessage(msg slackmessages.SlackMessage) bool {
	if strings.TrimSpace(msg.Text) == "" {
		return true
	}
	if s.minMessageLength > 0 && len([]rune(strings.TrimSpace(msg.Text))) < s.minMessageLength {
		return true
	}
	return false
}

func (s *SlackVectorizerService) logErrors(errs []*types.ProcessingError) {
	for _, err := range errs {
		if err == nil {
			continue
		}
		s.logger.Printf("vectorization error: type=%s message=%s file=%s retryable=%v",
			err.Type, err.Message, err.FilePath, err.Retryable)
	}
}

func (stats *ProcessingStats) finalize() {
	stats.EndTime = time.Now()
}

func (stats *ProcessingStats) incrementProcessed() {
	stats.mu.Lock()
	stats.MessagesProcessed++
	stats.mu.Unlock()
}

func (stats *ProcessingStats) incrementSkipped() {
	stats.mu.Lock()
	stats.MessagesSkipped++
	stats.mu.Unlock()
}

func (stats *ProcessingStats) addRetries(n int) {
	if n <= 0 {
		return
	}
	stats.mu.Lock()
	stats.Retries += n
	stats.mu.Unlock()
}

func (stats *ProcessingStats) incrementFailed() {
	stats.mu.Lock()
	stats.MessagesFailed++
	stats.mu.Unlock()
}

func (stats *ProcessingStats) appendError(err *types.ProcessingError) {
	if err == nil {
		return
	}
	stats.mu.Lock()
	stats.Errors = append(stats.Errors, *err)
	stats.mu.Unlock()
}

func groupMessagesByChannel(messages []slackmessages.SlackMessage) map[string][]slackmessages.SlackMessage {
	grouped := make(map[string][]slackmessages.SlackMessage)
	for _, msg := range messages {
		grouped[msg.ChannelID] = append(grouped[msg.ChannelID], msg)
	}
	return grouped
}
