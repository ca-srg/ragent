package slackbot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/slack-go/slack"

	"github.com/ca-srg/ragent/internal/slackmessages"
)

const realtimeVectorizeTimeout = 15 * time.Second

type realtimeVectorizer interface {
	VectorizeRealtime(ctx context.Context, message slackmessages.SlackMessage, dryRun bool) error
}

// VectorizeOptions configures realtime Slack vectorization.
type VectorizeOptions struct {
	Enabled          bool
	Channels         []string
	ExcludeBots      bool
	MinMessageLength int
}

type infoClient interface {
	GetConversationInfo(input *slack.GetConversationInfoInput) (*slack.Channel, error)
	GetUserInfo(userID string) (*slack.User, error)
	GetPermalink(params *slack.PermalinkParameters) (string, error)
}

type vectorizeSupport struct {
	client infoClient
	logger *log.Logger

	vectorizer realtimeVectorizer
	cfg        vectorizeConfig

	tracker      *vectorizeTracker
	channelCache sync.Map // map[string]*slack.Channel
	userCache    sync.Map // map[string]*slack.User
}

type vectorizeConfig struct {
	enabled     bool
	excludeBots bool
	minLength   int
	channels    map[string]struct{}
}

func newVectorizeSupport(client infoClient, logger *log.Logger) *vectorizeSupport {
	return &vectorizeSupport{
		client:  client,
		logger:  logger,
		tracker: newVectorizeTracker(),
		cfg:     vectorizeConfig{},
	}
}

func (v *vectorizeSupport) configure(vectorizer realtimeVectorizer, opts VectorizeOptions) {
	v.cfg = toVectorizeConfig(opts)
	if !v.cfg.enabled || vectorizer == nil {
		v.vectorizer = nil
		v.tracker = newVectorizeTracker()
		return
	}
	v.vectorizer = vectorizer
	v.tracker = newVectorizeTracker()
}

func toVectorizeConfig(opts VectorizeOptions) vectorizeConfig {
	channels := make(map[string]struct{})
	for _, ch := range opts.Channels {
		id := strings.TrimSpace(ch)
		if id == "" {
			continue
		}
		channels[id] = struct{}{}
	}
	return vectorizeConfig{
		enabled:     opts.Enabled,
		excludeBots: opts.ExcludeBots,
		minLength:   opts.MinMessageLength,
		channels:    channels,
	}
}

func (v *vectorizeSupport) shouldVectorize(botUserID string, evt *slack.MessageEvent) bool {
	if v == nil || v.vectorizer == nil || !v.cfg.enabled || evt == nil {
		return false
	}

	if evt.User == "" && evt.BotID == "" {
		return false
	}
	if botUserID != "" && evt.User == botUserID {
		return false
	}
	if len(v.cfg.channels) > 0 {
		if _, ok := v.cfg.channels[evt.Channel]; !ok {
			return false
		}
	}
	if v.cfg.excludeBots && (evt.BotID != "" || evt.Msg.SubType == slack.MsgSubTypeBotMessage) {
		return false
	}
	text := strings.TrimSpace(evt.Text)
	if text == "" {
		return false
	}
	if v.cfg.minLength > 0 && utf8.RuneCountInString(text) < v.cfg.minLength {
		return false
	}
	return true
}

func (v *vectorizeSupport) vectorize(evt *slack.MessageEvent) {
	if v == nil || v.vectorizer == nil || !v.cfg.enabled || evt == nil {
		return
	}

	vectorKey := fmt.Sprintf("%s-%s", evt.Channel, evt.Timestamp)
	if !v.tracker.mark(vectorKey) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), realtimeVectorizeTimeout)
	defer cancel()

	message, err := v.buildSlackMessage(evt)
	if err != nil {
		v.logf("vectorize_build_error channel=%s ts=%s err=%v", evt.Channel, evt.Timestamp, err)
		return
	}

	if err := v.vectorizer.VectorizeRealtime(ctx, message, false); err != nil {
		v.logf("vectorize_realtime_error vector_id=%s err=%v", message.GenerateVectorID(), err)
	}
}

func (v *vectorizeSupport) buildSlackMessage(evt *slack.MessageEvent) (slackmessages.SlackMessage, error) {
	channel, err := v.lookupChannel(evt.Channel)
	if err != nil {
		return slackmessages.SlackMessage{}, fmt.Errorf("get channel %s: %w", evt.Channel, err)
	}

	channelName := evt.Channel
	if channel != nil {
		if channel.NameNormalized != "" {
			channelName = channel.NameNormalized
		} else if channel.Name != "" {
			channelName = channel.Name
		}
	}

	userName := ""
	if evt.User != "" {
		if user, err := v.lookupUser(evt.User); err != nil {
			v.logf("vectorize_user_lookup_error user=%s err=%v", evt.User, err)
		} else if user != nil {
			userName = selectDisplayName(user)
		}
	}
	if userName == "" {
		userName = evt.Username
	}
	if userName == "" {
		userName = evt.User
	}

	permalink := ""
	if evt.Channel != "" && evt.Timestamp != "" {
		if link, err := v.client.GetPermalink(&slack.PermalinkParameters{
			Channel: evt.Channel,
			Ts:      evt.Timestamp,
		}); err == nil {
			permalink = link
		} else {
			v.logf("vectorize_permalink_error channel=%s ts=%s err=%v", evt.Channel, evt.Timestamp, err)
		}
	}

	var editedAt *time.Time
	if evt.Msg.Edited != nil && evt.Msg.Edited.Timestamp != "" {
		ts := parseSlackTimestamp(evt.Msg.Edited.Timestamp)
		if !ts.IsZero() {
			editedAt = &ts
		}
	}

	slackMsg := slackmessages.SlackMessage{
		ChannelID:        evt.Channel,
		ChannelName:      channelName,
		UserID:           evt.User,
		UserName:         userName,
		Text:             evt.Text,
		Timestamp:        evt.Timestamp,
		ThreadTimestamp:  evt.ThreadTimestamp,
		ParentUserID:     evt.Msg.ParentUserId,
		Permalink:        permalink,
		TeamID:           evt.Msg.Team,
		AppID:            extractAppIDFromMsg(evt.Msg),
		BotID:            evt.BotID,
		Language:         "",
		IsBot:            evt.BotID != "" || evt.Msg.SubType == slack.MsgSubTypeBotMessage,
		IsThreadReply:    evt.ThreadTimestamp != "" && evt.ThreadTimestamp != evt.Timestamp,
		EditedTimestamp:  editedAt,
		EditedUserID:     "",
		ReplyCount:       evt.Msg.ReplyCount,
		ReplyUsers:       append([]string(nil), evt.Msg.ReplyUsers...),
		Files:            convertSlackFiles(evt.Msg.Files),
		Reactions:        convertSlackReactions(evt.Msg.Reactions),
		LastReadAt:       nil,
		ThreadRootUserID: "",
	}
	if evt.Msg.Edited != nil {
		slackMsg.EditedUserID = evt.Msg.Edited.User
	}

	return slackMsg, nil
}

func (v *vectorizeSupport) lookupChannel(id string) (*slack.Channel, error) {
	if id == "" {
		return &slack.Channel{}, nil
	}
	if cached, ok := v.channelCache.Load(id); ok {
		if ch, ok := cached.(*slack.Channel); ok && ch != nil {
			return ch, nil
		}
	}
	channel, err := v.client.GetConversationInfo(&slack.GetConversationInfoInput{
		ChannelID: id,
	})
	if err != nil {
		return nil, err
	}
	if channel != nil {
		v.channelCache.Store(id, channel)
	}
	return channel, nil
}

func (v *vectorizeSupport) lookupUser(id string) (*slack.User, error) {
	if id == "" {
		return nil, nil
	}
	if cached, ok := v.userCache.Load(id); ok {
		if user, ok := cached.(*slack.User); ok {
			return user, nil
		}
	}
	user, err := v.client.GetUserInfo(id)
	if err != nil {
		return nil, err
	}
	if user != nil {
		v.userCache.Store(id, user)
	}
	return user, nil
}

func (v *vectorizeSupport) logf(format string, args ...interface{}) {
	if v.logger == nil {
		return
	}
	v.logger.Printf(format, args...)
}

type vectorizeTracker struct {
	seen sync.Map
}

func newVectorizeTracker() *vectorizeTracker {
	return &vectorizeTracker{}
}

func (t *vectorizeTracker) mark(id string) bool {
	if id == "" {
		return false
	}
	_, loaded := t.seen.LoadOrStore(id, struct{}{})
	return !loaded
}

func selectDisplayName(u *slack.User) string {
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

func convertSlackFiles(files []slack.File) []slackmessages.SlackFile {
	if len(files) == 0 {
		return nil
	}
	result := make([]slackmessages.SlackFile, 0, len(files))
	for _, file := range files {
		result = append(result, slackmessages.SlackFile{
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
	return result
}

func convertSlackReactions(reactions []slack.ItemReaction) []slackmessages.SlackReaction {
	if len(reactions) == 0 {
		return nil
	}
	result := make([]slackmessages.SlackReaction, 0, len(reactions))
	for _, reaction := range reactions {
		result = append(result, slackmessages.SlackReaction{
			Name:  reaction.Name,
			Count: reaction.Count,
			Users: append([]string(nil), reaction.Users...),
		})
	}
	return result
}

func parseSlackTimestamp(ts string) time.Time {
	parts := strings.Split(ts, ".")
	if len(parts) == 0 {
		return time.Time{}
	}
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

func parseInt64(v string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
}

func extractAppIDFromMsg(msg slack.Msg) string {
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
