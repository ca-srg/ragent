package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/ca-srg/ragent/internal/search"
	"github.com/ca-srg/ragent/internal/slacksearch"
	commontypes "github.com/ca-srg/ragent/internal/types"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateChatResponseIncludesSlackContext(t *testing.T) {
	originalServiceFactory := newHybridSearchServiceFunc
	originalSlackRunner := slackSearchRunner
	defer func() {
		newHybridSearchServiceFunc = originalServiceFactory
		slackSearchRunner = originalSlackRunner
	}()

	stubService := &stubHybridService{
		response: &search.SearchResponse{
			ContextParts: []string{"Document: Guidelines"},
			References:   map[string]string{"doc-1": "Guidelines"},
			TotalResults: 1,
		},
	}

	newHybridSearchServiceFunc = func(cfg *commontypes.Config, embeddingClient *bedrock.BedrockClient) (hybridSearchInitializer, error) {
		stubService.config = cfg
		return stubService, nil
	}

	progressCalls := 0
	slackSearchRunner = func(ctx context.Context, cfg *commontypes.Config, awsCfg aws.Config, embeddingClient opensearch.EmbeddingClient, userQuery string, channels []string, progressHandler func(iteration, max int)) (*slacksearch.SlackSearchResult, error) {
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
	cfg := &commontypes.Config{
		SlackSearchEnabled:            true,
		SlackSearchMaxResults:         5,
		SlackSearchMaxContextMessages: 10,
		SlackSearchMaxIterations:      2,
		SlackSearchTimeoutSeconds:     5,
	}

	awsCfg := aws.Config{Region: "us-west-2"}

	var resp string
	output := captureOutput(t, func() {
		var err error
		resp, err = generateChatResponse(
			"How do we deploy?",
			nil,
			chatClient,
			&bedrock.BedrockClient{},
			cfg,
			awsCfg,
			true,
		)
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Searching documents and Slack conversations...")
	assert.Contains(t, output, "Refining Slack search (iteration 1/2)...")
	assert.Contains(t, output, "Slack search completed in 2 iteration(s).")
	assert.Contains(t, output, "=== Slack Conversations ===")
	assert.True(t, strings.HasPrefix(resp, "All good"))
	assert.Contains(t, resp, "## Slack Conversations")
	assert.Contains(t, resp, "https://slack.example.com/archives/C001/p1700000000000")

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
	originalServiceFactory := newHybridSearchServiceFunc
	originalSlackRunner := slackSearchRunner
	defer func() {
		newHybridSearchServiceFunc = originalServiceFactory
		slackSearchRunner = originalSlackRunner
	}()

	stubService := &stubHybridService{
		response: &search.SearchResponse{
			ContextParts: []string{"Only docs"},
			TotalResults: 1,
		},
	}

	newHybridSearchServiceFunc = func(cfg *commontypes.Config, embeddingClient *bedrock.BedrockClient) (hybridSearchInitializer, error) {
		return stubService, nil
	}

	slackSearchRunner = func(ctx context.Context, cfg *commontypes.Config, awsCfg aws.Config, embeddingClient opensearch.EmbeddingClient, userQuery string, channels []string, progressHandler func(int, int)) (*slacksearch.SlackSearchResult, error) {
		t.Fatalf("slackSearchRunner should not be called when Slack disabled")
		return nil, nil
	}

	chatClient := &stubChatClient{response: "Done"}
	cfg := &commontypes.Config{SlackSearchEnabled: false}
	awsCfg := aws.Config{Region: "us-west-2"}

	var resp string
	output := captureOutput(t, func() {
		var err error
		resp, err = generateChatResponse(
			"Status?",
			nil,
			chatClient,
			&bedrock.BedrockClient{},
			cfg,
			awsCfg,
			false,
		)
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Searching documents...")
	assert.NotContains(t, output, "Slack search completed")
	assert.Equal(t, "Done", resp)
	require.Len(t, chatClient.messages, 1)
	lastMessage := chatClient.messages[0][len(chatClient.messages[0])-1]
	assert.NotContains(t, lastMessage.Content, "Slack Conversations")
}

type stubHybridService struct {
	config      *commontypes.Config
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
