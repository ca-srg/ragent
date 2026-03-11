package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/ca-srg/ragent/internal/pkg/embedding"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/pkg/opensearch"
	"github.com/ca-srg/ragent/internal/pkg/search"
	"github.com/ca-srg/ragent/internal/pkg/slacksearch"
	queryimpl "github.com/ca-srg/ragent/internal/query"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appconfig "github.com/ca-srg/ragent/internal/pkg/config"
)

func TestGenerateChatResponseIncludesSlackContext(t *testing.T) {
	originalServiceFactory := queryimpl.NewHybridSearchServiceFunc
	originalSlackRunner := queryimpl.SlackSearchRunner
	defer func() {
		queryimpl.NewHybridSearchServiceFunc = originalServiceFactory
		queryimpl.SlackSearchRunner = originalSlackRunner
	}()

	stubService := &stubHybridService{
		response: &search.SearchResponse{
			ContextParts: []string{"Document: Guidelines"},
			References:   map[string]string{"doc-1": "Guidelines"},
			TotalResults: 1,
		},
	}

	queryimpl.NewHybridSearchServiceFunc = func(cfg *appconfig.Config, embeddingClient embedding.EmbeddingClient) (queryimpl.HybridSearchInitializer, error) {
		stubService.config = cfg
		return stubService, nil
	}

	progressCalls := 0
	queryimpl.SlackSearchRunner = func(ctx context.Context, cfg *appconfig.Config, awsCfg aws.Config, embeddingClient opensearch.EmbeddingClient, userQuery string, channels []string, progressHandler func(iteration, max int)) (*slacksearch.SlackSearchResult, error) {
		require.Equal(t, "How do we deploy?", userQuery)
		progressHandler(1, 2)
		progressHandler(2, 2)
		progressCalls += 2
		return &slacksearch.SlackSearchResult{
			IterationCount: 2,
			TotalMatches:   1,
			Queries:        []string{"deployment guide"},
			EnrichedMessages: []slacksearch.EnrichedMessage{
				{
					OriginalMessage: slack.Message{
						Msg: slack.Msg{
							Channel:   "C001",
							Timestamp: "1700000000.000",
							User:      "U001",
							Text:      "Deployment steps",
						},
					},
					Permalink: "https://slack.example.com/archives/C001/p1700000000000",
				},
			},
		}, nil
	}

	chatClient := &stubChatClient{response: "All good"}
	cfg := &appconfig.Config{
		SlackSearchEnabled:            true,
		SlackSearchMaxResults:         5,
		SlackSearchMaxContextMessages: 10,
		SlackSearchMaxIterations:      2,
		SlackSearchTimeoutSeconds:     5,
	}

	awsCfg := aws.Config{Region: "us-west-2"}

	var result *queryimpl.ChatResult
	output := captureOutput(t, func() {
		var err error
		result, err = queryimpl.GenerateChatResponse(
			"How do we deploy?",
			nil,
			chatClient,
			&bedrock.BedrockClient{},
			cfg,
			awsCfg,
			true,
			queryimpl.ChatOptions{ContextSize: 5, BM25Weight: 0.5, VectorWeight: 0.5, UseJapaneseNLP: true},
		)
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Searching documents and Slack conversations...")
	assert.Contains(t, output, "Refining Slack search (iteration 1/2)...")
	assert.Contains(t, output, "Slack search completed in 2 iteration(s).")
	assert.Contains(t, output, "=== Slack Conversations ===")
	require.NotNil(t, result)
	assert.True(t, strings.HasPrefix(result.Response, "All good"))
	assert.Contains(t, result.Response, "## Slack Conversations")
	assert.Contains(t, result.Response, "https://slack.example.com/archives/C001/p1700000000000")

	require.Len(t, chatClient.messages, 1)
	lastMessage := chatClient.messages[0][len(chatClient.messages[0])-1]
	assert.Contains(t, lastMessage.Content, "Slack Conversations:")
	assert.Contains(t, lastMessage.Content, "#C001")
	assert.Equal(t, 2, progressCalls)
	require.NotNil(t, stubService.lastRequest)
	assert.Equal(t, "How do we deploy?", stubService.lastRequest.Query)
	assert.True(t, stubService.initCalled)
}

func TestGenerateChatResponseWithoutSlack(t *testing.T) {
	originalServiceFactory := queryimpl.NewHybridSearchServiceFunc
	originalSlackRunner := queryimpl.SlackSearchRunner
	defer func() {
		queryimpl.NewHybridSearchServiceFunc = originalServiceFactory
		queryimpl.SlackSearchRunner = originalSlackRunner
	}()

	stubService := &stubHybridService{
		response: &search.SearchResponse{
			ContextParts: []string{"Only docs"},
			TotalResults: 1,
		},
	}

	queryimpl.NewHybridSearchServiceFunc = func(cfg *appconfig.Config, embeddingClient embedding.EmbeddingClient) (queryimpl.HybridSearchInitializer, error) {
		return stubService, nil
	}

	queryimpl.SlackSearchRunner = func(ctx context.Context, cfg *appconfig.Config, awsCfg aws.Config, embeddingClient opensearch.EmbeddingClient, userQuery string, channels []string, progressHandler func(int, int)) (*slacksearch.SlackSearchResult, error) {
		t.Fatalf("slackSearchRunner should not be called when Slack disabled")
		return nil, nil
	}

	chatClient := &stubChatClient{response: "Done"}
	cfg := &appconfig.Config{SlackSearchEnabled: false}
	awsCfg := aws.Config{Region: "us-west-2"}

	var result *queryimpl.ChatResult
	output := captureOutput(t, func() {
		var err error
		result, err = queryimpl.GenerateChatResponse(
			"Status?",
			nil,
			chatClient,
			&bedrock.BedrockClient{},
			cfg,
			awsCfg,
			false,
			queryimpl.ChatOptions{ContextSize: 5, BM25Weight: 0.5, VectorWeight: 0.5},
		)
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Searching documents...")
	assert.NotContains(t, output, "Slack search completed")
	require.NotNil(t, result)
	assert.Equal(t, "Done", result.Response)
	require.Len(t, chatClient.messages, 1)
	lastMessage := chatClient.messages[0][len(chatClient.messages[0])-1]
	assert.NotContains(t, lastMessage.Content, "Slack Conversations")
}

type stubHybridService struct {
	config      *appconfig.Config
	response    *search.SearchResponse
	initCalled  bool
	lastRequest *search.SearchRequest
}

func (s *stubHybridService) Initialize(ctx context.Context) error {
	s.initCalled = true
	return nil
}

func (s *stubHybridService) Search(ctx context.Context, request *search.SearchRequest) (*search.SearchResponse, error) {
	s.lastRequest = request
	return s.response, nil
}

type stubChatClient struct {
	response string
	messages [][]bedrock.ChatMessage
}

func (s *stubChatClient) GenerateChatResponse(ctx context.Context, messages []bedrock.ChatMessage) (string, error) {
	copyMessages := make([]bedrock.ChatMessage, len(messages))
	copy(copyMessages, messages)
	s.messages = append(s.messages, copyMessages)
	return s.response, nil
}
