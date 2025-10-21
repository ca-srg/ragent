package slackmessages

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/slack-go/slack"
	"golang.org/x/time/rate"
)

// SlackAPI defines the subset of Slack Web API used by the MessageFetcher.
type SlackAPI interface {
	GetConversations(params *slack.GetConversationsParameters) ([]slack.Channel, string, error)
	GetConversationInfo(input *slack.GetConversationInfoInput) (*slack.Channel, error)
	GetConversationHistory(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	GetUserInfo(userID string) (*slack.User, error)
	GetPermalink(params *slack.PermalinkParameters) (string, error)
}

// MessageFetcher retrieves Slack messages according to the provided FetchConfig.
type MessageFetcher struct {
	client      SlackAPI
	limiter     *rate.Limiter
	maxRetries  int
	backoffBase time.Duration
	logger      *log.Logger

	userCache   map[string]*slack.User
	userCacheMu sync.RWMutex
}

// FetcherOption configures MessageFetcher.
type FetcherOption func(*MessageFetcher)

// WithRateLimiter overrides the default rate limiter.
func WithRateLimiter(l *rate.Limiter) FetcherOption {
	return func(f *MessageFetcher) {
		f.limiter = l
	}
}

// WithMaxRetries overrides the default retry attempts.
func WithMaxRetries(n int) FetcherOption {
	return func(f *MessageFetcher) {
		if n > 0 {
			f.maxRetries = n
		}
	}
}

// WithBackoffBase overrides the initial backoff duration for retries.
func WithBackoffBase(d time.Duration) FetcherOption {
	return func(f *MessageFetcher) {
		if d > 0 {
			f.backoffBase = d
		}
	}
}

// WithLogger overrides the default logger.
func WithLogger(l *log.Logger) FetcherOption {
	return func(f *MessageFetcher) {
		if l != nil {
			f.logger = l
		}
	}
}

// NewMessageFetcher constructs a MessageFetcher with sensible defaults.
func NewMessageFetcher(client SlackAPI, opts ...FetcherOption) *MessageFetcher {
	fetcher := &MessageFetcher{
		client:      client,
		limiter:     rate.NewLimiter(rate.Every(time.Minute), 1), // 1 request per minute by default
		maxRetries:  3,
		backoffBase: time.Second,
		logger:      log.New(os.Stdout, "slack-fetcher ", log.LstdFlags),
		userCache:   make(map[string]*slack.User),
	}
	for _, opt := range opts {
		opt(fetcher)
	}
	return fetcher
}

// FetchMessages retrieves messages for the specified configuration.
func (f *MessageFetcher) FetchMessages(ctx context.Context, cfg FetchConfig) ([]SlackMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	channels, err := f.resolveChannels(ctx, cfg.ChannelIDs)
	if err != nil {
		return nil, err
	}

	var allMessages []SlackMessage
	for _, ch := range channels {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		messages, err := f.fetchChannelMessages(ctx, ch, cfg)
		if err != nil {
			return nil, err
		}
		allMessages = append(allMessages, messages...)
	}

	return allMessages, nil
}

func (f *MessageFetcher) resolveChannels(ctx context.Context, requested []string) ([]slack.Channel, error) {
	if len(requested) == 0 {
		return f.listAllChannels(ctx)
	}

	var channels []slack.Channel
	for _, id := range requested {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		channel, err := f.getChannelInfo(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get channel info %s: %w", id, err)
		}
		channels = append(channels, *channel)
	}
	return channels, nil
}

func (f *MessageFetcher) listAllChannels(ctx context.Context) ([]slack.Channel, error) {
	var (
		channels []slack.Channel
		cursor   string
	)
	for {
		params := &slack.GetConversationsParameters{
			Cursor:          cursor,
			ExcludeArchived: true,
			Limit:           200,
			Types:           []string{"public_channel", "private_channel"},
		}
		result, nextCursor, err := f.getConversations(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("list conversations: %w", err)
		}
		channels = append(channels, result...)
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return channels, nil
}

func (f *MessageFetcher) fetchChannelMessages(ctx context.Context, channel slack.Channel, cfg FetchConfig) ([]SlackMessage, error) {
	pageSize := cfg.PageSize
	if pageSize <= 0 {
		pageSize = 200
	}

	var (
		allMessages []SlackMessage
		cursor      string
		remaining   = cfg.Limit
	)

	for {
		params := &slack.GetConversationHistoryParameters{
			ChannelID:          channel.ID,
			Cursor:             cursor,
			Limit:              pageSize,
			IncludeAllMetadata: true,
		}
		if cfg.From != nil && !cfg.From.IsZero() {
			params.Oldest = toSlackTimestamp(*cfg.From)
		}
		if cfg.To != nil && !cfg.To.IsZero() {
			params.Latest = toSlackTimestamp(*cfg.To)
		}
		if remaining > 0 && remaining < pageSize {
			params.Limit = remaining
		}

		resp, err := f.getConversationHistory(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("history channel=%s: %w", channel.ID, err)
		}

		var processed int
		for _, msg := range resp.Messages {
			if remaining > 0 && len(allMessages) >= cfg.Limit {
				return allMessages, nil
			}
			if !f.shouldIncludeMessage(msg, cfg) {
				continue
			}
			slackMsg, err := f.buildSlackMessage(ctx, channel, msg, false)
			if err != nil {
				return nil, err
			}
			allMessages = append(allMessages, slackMsg)
			processed++

			if cfg.IncludeThreads && msg.ThreadTimestamp != "" && msg.ThreadTimestamp == msg.Timestamp {
				threadMsgs, err := f.fetchThreadReplies(ctx, channel, msg, cfg)
				if err != nil {
					return nil, err
				}
				allMessages = append(allMessages, threadMsgs...)
			}
		}

		if !resp.HasMore || resp.ResponseMetaData.NextCursor == "" {
			break
		}
		if remaining > 0 && len(allMessages) >= cfg.Limit {
			break
		}
		cursor = resp.ResponseMetaData.NextCursor
		if processed == 0 {
			// No progress, avoid potential infinite loop
			break
		}
	}

	return allMessages, nil
}

func (f *MessageFetcher) fetchThreadReplies(ctx context.Context, channel slack.Channel, parent slack.Message, cfg FetchConfig) ([]SlackMessage, error) {
	var (
		cursor string
		result []SlackMessage
	)

	for {
		params := &slack.GetConversationRepliesParameters{
			ChannelID:          channel.ID,
			Timestamp:          parent.ThreadTimestamp,
			Cursor:             cursor,
			IncludeAllMetadata: true,
		}
		messages, hasMore, next, err := f.getConversationReplies(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("thread messages channel=%s ts=%s: %w", channel.ID, parent.ThreadTimestamp, err)
		}

		for _, msg := range messages {
			// Slack includes the parent message as part of replies; skip duplicates.
			if msg.Timestamp == parent.Timestamp {
				continue
			}
			if !f.shouldIncludeMessage(msg, cfg) {
				continue
			}
			slackMsg, err := f.buildSlackMessage(ctx, channel, msg, true)
			if err != nil {
				return nil, err
			}
			result = append(result, slackMsg)
		}

		if !hasMore || next == "" {
			break
		}
		cursor = next
	}

	return result, nil
}

func (f *MessageFetcher) buildSlackMessage(ctx context.Context, channel slack.Channel, msg slack.Message, isThreadReply bool) (SlackMessage, error) {
	userName := ""
	if msg.User != "" {
		user, err := f.getUser(ctx, msg.User)
		if err != nil {
			return SlackMessage{}, fmt.Errorf("get user %s: %w", msg.User, err)
		}
		if user != nil {
			userName = selectUserName(user)
		}
	}

	permalink := msg.Permalink
	if permalink == "" {
		pl, err := f.getPermalink(ctx, channel.ID, msg.Timestamp)
		if err == nil {
			permalink = pl
		} else {
			f.logf("permalink_error channel=%s ts=%s err=%v", channel.ID, msg.Timestamp, err)
		}
	}

	var editedAt *time.Time
	if msg.Edited != nil && msg.Edited.Timestamp != "" {
		ts := parseSlackTimestamp(msg.Edited.Timestamp)
		if !ts.IsZero() {
			editedAt = &ts
		}
	}

	var lastReadAt *time.Time
	if msg.LastRead != "" {
		ts := parseSlackTimestamp(msg.LastRead)
		if !ts.IsZero() {
			lastReadAt = &ts
		}
	}

	files := make([]SlackFile, 0, len(msg.Files))
	for _, file := range msg.Files {
		files = append(files, SlackFile{
			ID:                 file.ID,
			Name:               file.Name,
			Title:              file.Title,
			Filetype:           file.Filetype,
			URLPrivate:         file.URLPrivate,
			URLPrivateDownload: file.URLPrivateDownload,
			Permalink:          file.Permalink,
			Size:               file.Size,
		})
	}

	reactions := make([]SlackReaction, 0, len(msg.Reactions))
	for _, reaction := range msg.Reactions {
		reactions = append(reactions, SlackReaction{
			Name:  reaction.Name,
			Count: reaction.Count,
			Users: append([]string(nil), reaction.Users...),
		})
	}

	channelName := channel.NameNormalized
	if channelName == "" {
		channelName = channel.Name
	}

	slackMsg := SlackMessage{
		ChannelID:        channel.ID,
		ChannelName:      channelName,
		UserID:           msg.User,
		UserName:         userName,
		Text:             msg.Text,
		Timestamp:        msg.Timestamp,
		ThreadTimestamp:  msg.ThreadTimestamp,
		ParentUserID:     msg.ParentUserId,
		Permalink:        permalink,
		TeamID:           msg.Team,
		AppID:            extractAppID(msg),
		BotID:            msg.BotID,
		Language:         "", // Slack does not expose language for messages
		IsBot:            isBotMessage(msg),
		IsThreadReply:    isThreadReply,
		EditedTimestamp:  editedAt,
		EditedUserID:     "",
		ReplyCount:       msg.ReplyCount,
		ReplyUsers:       append([]string(nil), msg.ReplyUsers...),
		Files:            files,
		Reactions:        reactions,
		LastReadAt:       lastReadAt,
		ThreadRootUserID: threadRootUser(msg),
	}
	if msg.Edited != nil {
		slackMsg.EditedUserID = msg.Edited.User
	}
	return slackMsg, nil
}

func (f *MessageFetcher) shouldIncludeMessage(msg slack.Message, cfg FetchConfig) bool {
	if cfg.ExcludeBots && isBotMessage(msg) {
		return false
	}
	if msg.Text == "" {
		return false
	}
	length := utf8.RuneCountInString(strings.TrimSpace(msg.Text))
	if cfg.MinMessageLength > 0 && length < cfg.MinMessageLength {
		return false
	}
	if msg.SubType != "" && msg.SubType != slack.MsgSubTypeFileShare {
		return false
	}
	return true
}

func (f *MessageFetcher) getConversations(ctx context.Context, params *slack.GetConversationsParameters) ([]slack.Channel, string, error) {
	var (
		result []slack.Channel
		cursor string
	)
	err := f.withRetry(ctx, "conversations.list", func() error {
		if err := f.waitRate(ctx); err != nil {
			return err
		}
		var err error
		result, cursor, err = f.client.GetConversations(params)
		return err
	})
	return result, cursor, err
}

func (f *MessageFetcher) getChannelInfo(ctx context.Context, id string) (*slack.Channel, error) {
	var ch *slack.Channel
	err := f.withRetry(ctx, "conversations.info", func() error {
		if err := f.waitRate(ctx); err != nil {
			return err
		}
		var err error
		ch, err = f.client.GetConversationInfo(&slack.GetConversationInfoInput{ChannelID: id})
		return err
	})
	return ch, err
}

func (f *MessageFetcher) getConversationHistory(ctx context.Context, params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
	var resp *slack.GetConversationHistoryResponse
	err := f.withRetry(ctx, "conversations.history", func() error {
		if err := f.waitRate(ctx); err != nil {
			return err
		}
		var err error
		resp, err = f.client.GetConversationHistory(params)
		return err
	})
	return resp, err
}

func (f *MessageFetcher) getConversationReplies(ctx context.Context, params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
	var (
		messages []slack.Message
		hasMore  bool
		cursor   string
	)
	err := f.withRetry(ctx, "conversations.replies", func() error {
		if err := f.waitRate(ctx); err != nil {
			return err
		}
		var err error
		messages, hasMore, cursor, err = f.client.GetConversationReplies(params)
		return err
	})
	return messages, hasMore, cursor, err
}

func (f *MessageFetcher) getPermalink(ctx context.Context, channelID, timestamp string) (string, error) {
	var permalink string
	err := f.withRetry(ctx, "chat.getPermalink", func() error {
		if err := f.waitRate(ctx); err != nil {
			return err
		}
		var err error
		permalink, err = f.client.GetPermalink(&slack.PermalinkParameters{
			Channel: channelID,
			Ts:      timestamp,
		})
		return err
	})
	return permalink, err
}

func (f *MessageFetcher) getUser(ctx context.Context, userID string) (*slack.User, error) {
	f.userCacheMu.RLock()
	if user, ok := f.userCache[userID]; ok {
		f.userCacheMu.RUnlock()
		return user, nil
	}
	f.userCacheMu.RUnlock()

	var user *slack.User
	err := f.withRetry(ctx, "users.info", func() error {
		if err := f.waitRate(ctx); err != nil {
			return err
		}
		var err error
		user, err = f.client.GetUserInfo(userID)
		return err
	})
	if err != nil {
		return nil, err
	}

	f.userCacheMu.Lock()
	f.userCache[userID] = user
	f.userCacheMu.Unlock()

	return user, nil
}

func (f *MessageFetcher) waitRate(ctx context.Context) error {
	if f.limiter == nil {
		return nil
	}
	return f.limiter.Wait(ctx)
}

func (f *MessageFetcher) withRetry(ctx context.Context, operation string, fn func() error) error {
	var lastErr error
	attempts := f.maxRetries
	if attempts <= 0 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		if !f.shouldRetry(err) || attempt == attempts-1 {
			break
		}

		wait := f.backoffBase * time.Duration(1<<attempt)
		if rle, ok := err.(*slack.RateLimitedError); ok {
			if retryAfter := time.Duration(rle.RetryAfter) * time.Second; retryAfter > wait {
				wait = retryAfter
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("%s failed after %d attempts: %w", operation, attempts, lastErr)
}

func (f *MessageFetcher) shouldRetry(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Temporary() {
		return true
	}
	if _, ok := err.(*slack.RateLimitedError); ok {
		return true
	}
	var statusErr interface{ StatusCode() int }
	if errors.As(err, &statusErr) {
		code := statusErr.StatusCode()
		return code >= 500
	}
	return false
}

func (f *MessageFetcher) logf(format string, args ...interface{}) {
	if f.logger == nil {
		return
	}
	f.logger.Printf(format, args...)
}

func selectUserName(u *slack.User) string {
	if u == nil {
		return ""
	}
	if u.Profile.DisplayName != "" {
		return u.Profile.DisplayName
	}
	if u.RealName != "" {
		return u.RealName
	}
	return u.Name
}

func isBotMessage(msg slack.Message) bool {
	if msg.BotID != "" {
		return true
	}
	if msg.SubType == slack.MsgSubTypeBotMessage {
		return true
	}
	if msg.SubType == slack.MsgSubTypeMessageChanged && msg.SubMessage != nil {
		return isBotMessage(slack.Message{Msg: *msg.SubMessage})
	}
	return false
}

func threadRootUser(msg slack.Message) string {
	if msg.Root != nil {
		return msg.Root.User
	}
	if msg.SubMessage != nil {
		return msg.SubMessage.User
	}
	return ""
}

func parseSlackTimestamp(ts string) time.Time {
	parts := strings.Split(ts, ".")
	seconds, err := parseInt64(parts[0])
	if err != nil {
		return time.Time{}
	}
	var nanos int64
	if len(parts) > 1 {
		frac := parts[1]
		if len(frac) > 9 {
			frac = frac[:9]
		}
		for len(frac) < 9 {
			frac += "0"
		}
		nanos, err = parseInt64(frac)
		if err != nil {
			nanos = 0
		}
	}
	return time.Unix(seconds, nanos).UTC()
}

func toSlackTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return fmt.Sprintf("%d.%06d", t.Unix(), t.Nanosecond()/1000)
}

func parseInt64(v string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
}

func extractAppID(msg slack.Message) string {
	if msg.BotProfile != nil && msg.BotProfile.AppID != "" {
		return msg.BotProfile.AppID
	}
	if msg.Metadata.EventPayload != nil {
		if appID, ok := msg.Metadata.EventPayload["app_id"].(string); ok {
			return appID
		}
	}
	return ""
}
