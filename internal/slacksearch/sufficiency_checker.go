package slacksearch

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const sufficiencyLLMTimeout = 10 * time.Second

var sufficiencySystemPrompt = strings.TrimSpace(`
You evaluate whether Slack conversation snippets contain enough information to answer a user query.
Respond in JSON using this schema:
{
  "is_sufficient": bool,
  "missing_info": ["string"],
  "reasoning": "string",
  "confidence": float
}
- "missing_info" should list concrete facts or clarifications still required.
`)

// SufficiencyRequest supplies the data needed for sufficiency evaluation.
type SufficiencyRequest struct {
	UserQuery     string
	Messages      []EnrichedMessage
	Iteration     int
	MaxIterations int
}

// SufficiencyResponse captures the sufficiency assessment outcome.
type SufficiencyResponse struct {
	IsSufficient bool
	MissingInfo  []string
	Reasoning    string
	Confidence   float64
}

// SufficiencyChecker uses an LLM to verify information completeness.
type SufficiencyChecker struct {
	bedrockClient bedrockChatClient
	logger        *log.Logger
	nowFunc       func() time.Time
}

// NewSufficiencyChecker creates a new SufficiencyChecker.
func NewSufficiencyChecker(bedrockClient *bedrock.BedrockClient, logger *log.Logger) *SufficiencyChecker {
	if logger == nil {
		logger = log.New(log.Default().Writer(), "slacksearch/sufficiency_checker ", log.LstdFlags)
	}
	return &SufficiencyChecker{
		bedrockClient: bedrockClient,
		logger:        logger,
		nowFunc:       time.Now,
	}
}

// Check determines whether the provided Slack context is sufficient.
func (s *SufficiencyChecker) Check(ctx context.Context, req *SufficiencyRequest) (*SufficiencyResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := slackSearchTracer.Start(ctx, "slacksearch.sufficiency.check")
	defer span.End()

	if req == nil {
		err := fmt.Errorf("sufficiency request cannot be nil")
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid_request")
		return nil, err
	}

	queryHash := telemetryFingerprint(req.UserQuery)
	span.SetAttributes(
		attribute.String("slack.query_hash", queryHash),
		attribute.Int("slack.message_count", len(req.Messages)),
		attribute.Int("slack.iteration", req.Iteration),
		attribute.Int("slack.max_iterations", req.MaxIterations),
	)

	if strings.TrimSpace(req.UserQuery) == "" {
		err := fmt.Errorf("user query cannot be empty")
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid_query")
		return nil, err
	}

	if req.MaxIterations > 0 && req.Iteration >= req.MaxIterations {
		resp := &SufficiencyResponse{
			IsSufficient: true,
			MissingInfo:  []string{"Maximum iteration limit reached; provide best-effort summary."},
			Reasoning:    "Iteration cap reached",
			Confidence:   0.3,
		}
		span.SetAttributes(
			attribute.Bool("slack.iteration_cap_reached", true),
			attribute.Bool("slack.sufficiency", resp.IsSufficient),
			attribute.Int("slack.missing_info_count", len(resp.MissingInfo)),
			attribute.Float64("slack.confidence", resp.Confidence),
		)
		return resp, nil
	}

	if s.bedrockClient == nil {
		resp := &SufficiencyResponse{
			IsSufficient: false,
			MissingInfo:  []string{"LLM client unavailable"},
			Reasoning:    "Cannot evaluate sufficiency without LLM",
			Confidence:   0,
		}
		span.SetAttributes(
			attribute.Bool("slack.llm_available", false),
			attribute.Bool("slack.sufficiency", resp.IsSufficient),
			attribute.Int("slack.missing_info_count", len(resp.MissingInfo)),
			attribute.Float64("slack.confidence", resp.Confidence),
		)
		return resp, nil
	}

	messages := s.buildPromptMessages(req)

	checkCtx, cancel := context.WithTimeout(ctx, sufficiencyLLMTimeout)
	defer cancel()

	raw, err := s.bedrockClient.GenerateChatResponse(checkCtx, messages)
	if err != nil {
		s.logger.Printf("SufficiencyChecker: LLM evaluation failed: %v", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "llm_evaluation_failed")
		return &SufficiencyResponse{
			IsSufficient: false,
			MissingInfo:  []string{"LLM evaluation failed; retry later"},
			Reasoning:    err.Error(),
			Confidence:   0,
		}, nil
	}

	cleaned := cleanLLMJSON(raw)
	s.logger.Printf("SufficiencyChecker: LLM response: %s", cleaned)

	var parsed struct {
		IsSufficient bool     `json:"is_sufficient"`
		MissingInfo  []string `json:"missing_info"`
		Reasoning    string   `json:"reasoning"`
		Confidence   *float64 `json:"confidence"`
	}

	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		s.logger.Printf("SufficiencyChecker: failed to parse LLM JSON: %v", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "response_parse_failed")
		return &SufficiencyResponse{
			IsSufficient: false,
			MissingInfo:  []string{"Unable to parse sufficiency response"},
			Reasoning:    err.Error(),
			Confidence:   0,
		}, nil
	}

	confidence := 0.0
	if parsed.Confidence != nil {
		confidence = *parsed.Confidence
	}

	resp := &SufficiencyResponse{
		IsSufficient: parsed.IsSufficient,
		MissingInfo:  coalesceStrings(parsed.MissingInfo),
		Reasoning:    strings.TrimSpace(parsed.Reasoning),
		Confidence:   confidence,
	}

	span.SetAttributes(
		attribute.Bool("slack.sufficiency", resp.IsSufficient),
		attribute.Int("slack.missing_info_count", len(resp.MissingInfo)),
		attribute.Float64("slack.confidence", resp.Confidence),
	)
	s.logger.Printf("SufficiencyChecker: completed hash=%s sufficient=%t confidence=%.2f missing=%d",
		queryHash, resp.IsSufficient, resp.Confidence, len(resp.MissingInfo))

	return resp, nil
}

func (s *SufficiencyChecker) buildPromptMessages(req *SufficiencyRequest) []bedrock.ChatMessage {
	var sb strings.Builder
	sb.WriteString("User query:\n")
	sb.WriteString(req.UserQuery)
	sb.WriteString("\n\nCriteria:\n")
	sb.WriteString("1. Does the information contain required specific facts (dates, numbers, names, decisions, locations, links)?\n")
	sb.WriteString("2. Can at least one reasonable interpretation of the question be answered?\n")
	sb.WriteString("3. Is the information coherent and non-contradictory?\n\n")
	sb.WriteString("Provide JSON as specified.\n\n")
	sb.WriteString("Messages:\n")

	for i, msg := range req.Messages {
		sb.WriteString(fmt.Sprintf("Message %d (channel=%s user=%s ts=%s):\n", i, msg.OriginalMessage.Channel, msg.OriginalMessage.User, msg.OriginalMessage.Timestamp))
		sb.WriteString(fmt.Sprintf("- Text: %s\n", strings.TrimSpace(msg.OriginalMessage.Text)))
		if len(msg.ThreadMessages) > 0 {
			sb.WriteString("- Thread replies:\n")
			for _, reply := range msg.ThreadMessages {
				sb.WriteString(fmt.Sprintf("    • [%s] %s: %s\n", reply.Timestamp, reply.User, strings.TrimSpace(reply.Text)))
			}
		}
		if len(msg.PreviousMessages) > 0 {
			sb.WriteString("- Previous context:\n")
			for _, prev := range msg.PreviousMessages {
				sb.WriteString(fmt.Sprintf("    • [%s] %s: %s\n", prev.Timestamp, prev.User, strings.TrimSpace(prev.Text)))
			}
		}
		if len(msg.NextMessages) > 0 {
			sb.WriteString("- Subsequent context:\n")
			for _, next := range msg.NextMessages {
				sb.WriteString(fmt.Sprintf("    • [%s] %s: %s\n", next.Timestamp, next.User, strings.TrimSpace(next.Text)))
			}
		}
		if msg.Permalink != "" {
			sb.WriteString(fmt.Sprintf("- Permalink: %s\n", msg.Permalink))
		}
		sb.WriteString("\n")
	}

	userContent := sb.String()

	return []bedrock.ChatMessage{
		{Role: "system", Content: sufficiencySystemPrompt},
		{Role: "user", Content: userContent},
	}
}

func coalesceStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(values))
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return []string{}
	}
	return result
}
