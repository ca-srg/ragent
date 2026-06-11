package slacksearch

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockQueryGenerator struct {
	mock.Mock
}

func (m *mockQueryGenerator) GenerateQueries(ctx context.Context, req *QueryGenerationRequest) (*QueryGenerationResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*QueryGenerationResponse)
	return resp, args.Error(1)
}

func (m *mockQueryGenerator) GenerateAlternativeQueries(ctx context.Context, req *QueryGenerationRequest) (*QueryGenerationResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*QueryGenerationResponse)
	return resp, args.Error(1)
}

type mockSearcher struct {
	mock.Mock
}

func (m *mockSearcher) SearchWithRetry(ctx context.Context, req *SearchRequest, maxRetries int) (*SearchResponse, error) {
	args := m.Called(ctx, req, maxRetries)
	resp, _ := args.Get(0).(*SearchResponse)
	return resp, args.Error(1)
}

type mockContextRetriever struct {
	mock.Mock
}

func (m *mockContextRetriever) RetrieveContext(ctx context.Context, req *ContextRequest) (*ContextResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*ContextResponse)
	return resp, args.Error(1)
}

type mockSufficiencyChecker struct {
	mock.Mock
}

func (m *mockSufficiencyChecker) Check(ctx context.Context, req *SufficiencyRequest) (*SufficiencyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*SufficiencyResponse)
	return resp, args.Error(1)
}

func newTestService(cfg *SlackSearchConfig) *SlackSearchService {
	return &SlackSearchService{
		config: cfg,
		logger: log.New(io.Discard, "", 0),
	}
}

func defaultConfig() *SlackSearchConfig {
	return &SlackSearchConfig{
		Enabled:              true,
		UserToken:            "xoxp-test",
		MaxResults:           5,
		MaxRetries:           0,
		ContextWindowMinutes: 30,
		MaxIterations:        3,
		MaxContextMessages:   10,
		TimeoutSeconds:       5,
	}
}

func slackMsg(ts, text string) slack.Message {
	return slack.Message{
		Msg: slack.Msg{
			Channel:   "C1",
			User:      "U1",
			Timestamp: ts,
			Text:      text,
		},
	}
}

func TestSlackSearchService_SearchSuccess(t *testing.T) {
	cfg := defaultConfig()
	service := newTestService(cfg)

	qg := &mockQueryGenerator{}
	searcher := &mockSearcher{}
	context := &mockContextRetriever{}
	suff := &mockSufficiencyChecker{}

	service.queryGenerator = qg
	service.userSearcher = searcher
	service.contextRetriever = context
	service.sufficiencyChecker = suff

	genResp := &QueryGenerationResponse{
		Queries: []string{"deployment summary"},
	}
	searchResp := &SearchResponse{
		Messages: []slack.Message{
			slackMsg("1697880000.000100", "Deployment completed successfully"),
		},
		TotalCount: 1,
	}
	enriched := []EnrichedMessage{
		{
			OriginalMessage: slackMsg("1697880000.000100", "Deployment completed successfully"),
			ThreadMessages:  nil,
			Permalink:       "https://example.com/1",
		},
	}

	qg.On("GenerateQueries", mock.Anything, mock.AnythingOfType("*slacksearch.QueryGenerationRequest")).Return(genResp, nil).Once()
	searcher.On("SearchWithRetry", mock.Anything, mock.MatchedBy(func(req *SearchRequest) bool {
		return req.Query == "deployment summary"
	}), cfg.MaxRetries).Return(searchResp, nil).Once()
	context.On("RetrieveContext", mock.Anything, mock.AnythingOfType("*slacksearch.ContextRequest")).
		Return(&ContextResponse{EnrichedMessages: enriched, TotalRetrieved: 1}, nil).Once()
	suff.On("Check", mock.Anything, mock.AnythingOfType("*slacksearch.SufficiencyRequest")).
		Return(&SufficiencyResponse{IsSufficient: true, MissingInfo: nil, Confidence: 0.8}, nil).Once()

	result, err := service.Search(contextBackground(), "How did the deployment go?", []string{"general"}, SearchOptions{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsSufficient)
	assert.Equal(t, 1, result.IterationCount)
	assert.Len(t, result.Queries, 1)
	assert.Len(t, result.EnrichedMessages, 1)
	assert.Contains(t, result.Sources, "C1#1697880000.000100")
	assert.GreaterOrEqual(t, int(result.ExecutionTime), 0)

	qg.AssertExpectations(t)
	searcher.AssertExpectations(t)
	context.AssertExpectations(t)
	suff.AssertExpectations(t)
}

func TestSlackSearchService_SearchIterativeRefinement(t *testing.T) {
	cfg := defaultConfig()
	cfg.MaxIterations = 3
	service := newTestService(cfg)

	qg := &mockQueryGenerator{}
	searcher := &mockSearcher{}
	context := &mockContextRetriever{}
	suff := &mockSufficiencyChecker{}

	service.queryGenerator = qg
	service.userSearcher = searcher
	service.contextRetriever = context
	service.sufficiencyChecker = suff

	firstGen := &QueryGenerationResponse{Queries: []string{"first attempt"}}
	secondGen := &QueryGenerationResponse{Queries: []string{"second attempt"}}
	searchResp := &SearchResponse{
		Messages:   []slack.Message{slackMsg("1697880000.000100", "Initial message")},
		TotalCount: 1,
	}
	contextResp := &ContextResponse{
		EnrichedMessages: []EnrichedMessage{{OriginalMessage: slackMsg("1697880000.000100", "Initial message")}},
		TotalRetrieved:   1,
	}

	qg.On("GenerateQueries", mock.Anything, mock.AnythingOfType("*slacksearch.QueryGenerationRequest")).Return(firstGen, nil).Once()
	qg.On("GenerateAlternativeQueries", mock.Anything, mock.AnythingOfType("*slacksearch.QueryGenerationRequest")).Return(secondGen, nil).Once()

	searcher.On("SearchWithRetry", mock.Anything, mock.AnythingOfType("*slacksearch.SearchRequest"), cfg.MaxRetries).
		Return(searchResp, nil).Twice()
	context.On("RetrieveContext", mock.Anything, mock.AnythingOfType("*slacksearch.ContextRequest")).Return(contextResp, nil).Twice()
	suff.On("Check", mock.Anything, mock.AnythingOfType("*slacksearch.SufficiencyRequest")).
		Return(&SufficiencyResponse{IsSufficient: false, MissingInfo: []string{"Need more details"}}, nil).Once()
	suff.On("Check", mock.Anything, mock.AnythingOfType("*slacksearch.SufficiencyRequest")).
		Return(&SufficiencyResponse{IsSufficient: true, MissingInfo: nil}, nil).Once()

	result, err := service.Search(contextBackground(), "Summarize the incident", nil, SearchOptions{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 2, result.IterationCount)
	assert.Len(t, result.Queries, 2)
	assert.True(t, result.IsSufficient)

	qg.AssertExpectations(t)
	searcher.AssertExpectations(t)
	context.AssertExpectations(t)
	suff.AssertExpectations(t)
}

func TestSlackSearchService_SearchNoResults(t *testing.T) {
	cfg := defaultConfig()
	service := newTestService(cfg)

	qg := &mockQueryGenerator{}
	searcher := &mockSearcher{}

	service.queryGenerator = qg
	service.userSearcher = searcher
	service.contextRetriever = &mockContextRetriever{}
	service.sufficiencyChecker = &mockSufficiencyChecker{}

	qg.On("GenerateQueries", mock.Anything, mock.AnythingOfType("*slacksearch.QueryGenerationRequest")).
		Return(&QueryGenerationResponse{Queries: []string{"no results"}}, nil).Once()
	searcher.On("SearchWithRetry", mock.Anything, mock.AnythingOfType("*slacksearch.SearchRequest"), cfg.MaxRetries).
		Return(&SearchResponse{Messages: nil, TotalCount: 0}, nil).Once()

	result, err := service.Search(contextBackground(), "Question with no matches", nil, SearchOptions{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsSufficient)
	require.NotEmpty(t, result.MissingInfo)
	assert.Contains(t, result.MissingInfo[0], "No Slack conversations matched")
	assert.Zero(t, result.TotalMatches)

	qg.AssertExpectations(t)
	searcher.AssertExpectations(t)
}

func TestSlackSearchService_SearchHandlesSearchErrors(t *testing.T) {
	cfg := defaultConfig()
	service := newTestService(cfg)

	qg := &mockQueryGenerator{}
	searcher := &mockSearcher{}

	service.queryGenerator = qg
	service.userSearcher = searcher
	service.contextRetriever = &mockContextRetriever{}
	service.sufficiencyChecker = &mockSufficiencyChecker{}

	qg.On("GenerateQueries", mock.Anything, mock.AnythingOfType("*slacksearch.QueryGenerationRequest")).
		Return(&QueryGenerationResponse{Queries: []string{"error"}}, nil).Once()
	searcher.On("SearchWithRetry", mock.Anything, mock.AnythingOfType("*slacksearch.SearchRequest"), cfg.MaxRetries).
		Return(nil, assert.AnError).Once()

	result, err := service.Search(contextBackground(), "Failing query", nil, SearchOptions{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsSufficient)
	assert.Contains(t, result.MissingInfo[0], "No Slack conversations matched")

	qg.AssertExpectations(t)
	searcher.AssertExpectations(t)
}

func TestSlackSearchService_SearchRespectsContextCancellation(t *testing.T) {
	cfg := defaultConfig()
	cfg.MaxIterations = 3
	service := newTestService(cfg)

	qg := &mockQueryGenerator{}
	searcher := &mockSearcher{}
	contextRetriever := &mockContextRetriever{}
	suff := &mockSufficiencyChecker{}

	service.queryGenerator = qg
	service.userSearcher = searcher
	service.contextRetriever = contextRetriever
	service.sufficiencyChecker = suff

	genResp := &QueryGenerationResponse{Queries: []string{"first"}}
	searchResp := &SearchResponse{
		Messages:   []slack.Message{slackMsg("1697880000.000100", "Message")},
		TotalCount: 1,
	}
	contextResp := &ContextResponse{
		EnrichedMessages: []EnrichedMessage{{OriginalMessage: slackMsg("1697880000.000100", "Message")}},
		TotalRetrieved:   1,
	}

	qg.On("GenerateQueries", mock.Anything, mock.AnythingOfType("*slacksearch.QueryGenerationRequest")).
		Return(genResp, nil).Once()
	searcher.On("SearchWithRetry", mock.Anything, mock.AnythingOfType("*slacksearch.SearchRequest"), cfg.MaxRetries).
		Return(searchResp, nil).Once()
	contextRetriever.On("RetrieveContext", mock.Anything, mock.AnythingOfType("*slacksearch.ContextRequest")).
		Return(contextResp, nil).Once()

	ctx, cancel := context.WithCancel(context.Background())
	suff.On("Check", mock.Anything, mock.AnythingOfType("*slacksearch.SufficiencyRequest")).Run(func(args mock.Arguments) {
		cancel()
	}).Return(&SufficiencyResponse{IsSufficient: false, MissingInfo: []string{"Need more"}}, nil).Once()

	result, err := service.Search(ctx, "Cancel after first iteration", nil, SearchOptions{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
	assert.Nil(t, result)

	qg.AssertExpectations(t)
	searcher.AssertExpectations(t)
	contextRetriever.AssertExpectations(t)
	suff.AssertExpectations(t)
}

func contextBackground() context.Context {
	return context.Background()
}

// --- selectSearcher / dispatch tests ---

func TestSlackSearchService_SelectSearcherPrefersUserWhenAvailable(t *testing.T) {
	s := newTestService(defaultConfig())
	userS := &mockSearcher{}
	asstS := &mockSearcher{}
	s.userSearcher = userS
	s.assistantSearcher = asstS

	sr, backend, err := s.selectSearcher(SearchOptions{ActionToken: "TK"})
	require.NoError(t, err)
	assert.Equal(t, SearchBackendUser, backend)
	// Compare interface identity by type assertion to *mockSearcher.
	gotMock, ok := sr.(*mockSearcher)
	require.True(t, ok)
	assert.Same(t, userS, gotMock)
}

func TestSlackSearchService_SelectSearcherFallsBackToUserWithoutActionToken(t *testing.T) {
	s := newTestService(defaultConfig())
	userS := &mockSearcher{}
	asstS := &mockSearcher{}
	s.userSearcher = userS
	s.assistantSearcher = asstS

	sr, backend, err := s.selectSearcher(SearchOptions{})
	require.NoError(t, err)
	assert.Equal(t, SearchBackendUser, backend)
	gotMock, ok := sr.(*mockSearcher)
	require.True(t, ok)
	assert.Same(t, userS, gotMock)
}

func TestSlackSearchService_SelectSearcherActionTokenButNoAssistantUsesUser(t *testing.T) {
	s := newTestService(defaultConfig())
	userS := &mockSearcher{}
	s.userSearcher = userS
	s.assistantSearcher = nil

	sr, backend, err := s.selectSearcher(SearchOptions{ActionToken: "TK"})
	require.NoError(t, err)
	assert.Equal(t, SearchBackendUser, backend)
	gotMock, ok := sr.(*mockSearcher)
	require.True(t, ok)
	assert.Same(t, userS, gotMock)
}

func TestSlackSearchService_SelectSearcherUsesAssistantWhenUserUnavailable(t *testing.T) {
	s := newTestService(defaultConfig())
	asstS := &mockSearcher{}
	s.userSearcher = nil
	s.assistantSearcher = asstS

	sr, backend, err := s.selectSearcher(SearchOptions{ActionToken: "TK"})
	require.NoError(t, err)
	assert.Equal(t, SearchBackendAssistant, backend)
	gotMock, ok := sr.(*mockSearcher)
	require.True(t, ok)
	assert.Same(t, asstS, gotMock)
}

func TestSlackSearchService_SelectSearcherWithNothingErrors(t *testing.T) {
	s := newTestService(defaultConfig())
	s.userSearcher = nil
	s.assistantSearcher = nil

	_, _, err := s.selectSearcher(SearchOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SLACK_USER_TOKEN")
}

func TestSlackSearchService_FallbackEnrichedPreservesPermalink(t *testing.T) {
	service := newTestService(defaultConfig())
	msg := slack.Message{Msg: slack.Msg{
		Channel:   "C1",
		Timestamp: "1700000000.000100",
		Text:      "hit",
		Permalink: "https://example.slack.com/archives/C1/p1700000000000100",
	}}

	enriched := service.fallbackEnriched([]slack.Message{msg})
	require.Len(t, enriched, 1)
	assert.Equal(t, msg.Permalink, enriched[0].Permalink)
}

func TestSlackSearchService_SearchPrefersUserBackendWhenActionTokenAndUserSearcher(t *testing.T) {
	cfg := defaultConfig()
	service := newTestService(cfg)

	qg := &mockQueryGenerator{}
	userS := &mockSearcher{}
	asstS := &mockSearcher{}
	contextRetriever := &mockContextRetriever{}
	suff := &mockSufficiencyChecker{}

	service.queryGenerator = qg
	service.userSearcher = userS
	service.assistantSearcher = asstS
	service.contextRetriever = contextRetriever
	service.sufficiencyChecker = suff

	msg := slackMsg("1700000000.000100", "user-token hit")
	enriched := []EnrichedMessage{{OriginalMessage: msg, Permalink: "https://example.com/user-hit"}}

	qg.On("GenerateQueries", mock.Anything, mock.AnythingOfType("*slacksearch.QueryGenerationRequest")).
		Return(&QueryGenerationResponse{Queries: []string{"q1"}}, nil).Once()
	userS.On("SearchWithRetry", mock.Anything, mock.MatchedBy(func(req *SearchRequest) bool {
		return req.ActionToken == "TK_FROM_EVENT" && req.Query == "q1"
	}), cfg.MaxRetries).Return(&SearchResponse{
		Messages:   []slack.Message{msg},
		TotalCount: 1,
	}, nil).Once()
	contextRetriever.On("RetrieveContext", mock.Anything, mock.AnythingOfType("*slacksearch.ContextRequest")).
		Return(&ContextResponse{EnrichedMessages: enriched, TotalRetrieved: 1}, nil).Once()
	suff.On("Check", mock.Anything, mock.AnythingOfType("*slacksearch.SufficiencyRequest")).
		Return(&SufficiencyResponse{IsSufficient: true, MissingInfo: nil, Confidence: 0.9}, nil).Once()

	result, err := service.Search(context.Background(), "find x", nil, SearchOptions{
		ActionToken: "TK_FROM_EVENT",
		ChannelID:   "C_ORIGIN",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.EnrichedMessages, 1)
	assert.Equal(t, "https://example.com/user-hit", result.Sources["C1#1700000000.000100"])

	userS.AssertExpectations(t)
	asstS.AssertNotCalled(t, "SearchWithRetry", mock.Anything, mock.Anything, mock.Anything)
	contextRetriever.AssertExpectations(t)
	qg.AssertExpectations(t)
	suff.AssertExpectations(t)
}

func TestSlackSearchService_SearchDispatchesToAssistantWhenUserUnavailable(t *testing.T) {
	cfg := defaultConfig()
	service := newTestService(cfg)

	qg := &mockQueryGenerator{}
	asstS := &mockSearcher{}
	contextRetriever := &mockContextRetriever{}
	suff := &mockSufficiencyChecker{}

	service.queryGenerator = qg
	service.userSearcher = nil
	service.assistantSearcher = asstS
	service.contextRetriever = contextRetriever
	service.sufficiencyChecker = suff

	msg := slack.Message{Msg: slack.Msg{
		Channel:   "C1",
		User:      "U1",
		Timestamp: "1700000000.000100",
		Text:      "hit",
		Permalink: "https://example.slack.com/archives/C1/p1700000000000100",
	}}
	prev := slackMsg("1700000000.000000", "before context")
	next := slackMsg("1700000000.000200", "after context")

	qg.On("GenerateQueries", mock.Anything, mock.AnythingOfType("*slacksearch.QueryGenerationRequest")).
		Return(&QueryGenerationResponse{Queries: []string{"q1"}}, nil).Once()

	// assistantSearcher must be called and receive ActionToken + ContextChannelID only when userSearcher is unavailable.
	asstS.On("SearchWithRetry", mock.Anything, mock.MatchedBy(func(req *SearchRequest) bool {
		return req.ActionToken == "TK_FROM_EVENT" && req.ContextChannelID == "C_ORIGIN"
	}), cfg.MaxRetries).Return(&SearchResponse{
		Messages: []slack.Message{msg},
		EnrichedMessages: []EnrichedMessage{
			{
				OriginalMessage:  msg,
				PreviousMessages: []slack.Message{prev},
				NextMessages:     []slack.Message{next},
				Permalink:        msg.Permalink,
			},
		},
		TotalCount: 1,
	}, nil).Once()

	suff.On("Check", mock.Anything, mock.MatchedBy(func(req *SufficiencyRequest) bool {
		return len(req.Messages) == 1 &&
			len(req.Messages[0].PreviousMessages) == 1 &&
			req.Messages[0].PreviousMessages[0].Text == "before context" &&
			len(req.Messages[0].NextMessages) == 1 &&
			req.Messages[0].NextMessages[0].Text == "after context"
	})).
		Return(&SufficiencyResponse{IsSufficient: true, MissingInfo: nil, Confidence: 0.9}, nil).Once()

	result, err := service.Search(context.Background(), "find x", nil, SearchOptions{
		ActionToken: "TK_FROM_EVENT",
		ChannelID:   "C_ORIGIN",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.EnrichedMessages, 1)
	assert.Equal(t, msg.Permalink, result.EnrichedMessages[0].Permalink)
	assert.Equal(t, msg.Permalink, result.Sources["C1#1700000000.000100"])
	assert.Equal(t, "before context", result.EnrichedMessages[0].PreviousMessages[0].Text)
	assert.Equal(t, "after context", result.EnrichedMessages[0].NextMessages[0].Text)

	asstS.AssertExpectations(t)
	contextRetriever.AssertNotCalled(t, "RetrieveContext", mock.Anything, mock.Anything)
	qg.AssertExpectations(t)
	suff.AssertExpectations(t)
}
