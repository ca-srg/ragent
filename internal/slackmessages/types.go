package slackmessages

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ca-srg/ragent/internal/types"
)

const (
	// SourceSlack identifies Slack as the document source.
	SourceSlack = "slack"

	// CategorySlackMessage is the DocumentMetadata category for Slack messages.
	CategorySlackMessage = "slack-message"
)

// SlackMessage represents a normalized Slack message enriched with metadata that is
// required for vectorization and downstream processing.
type SlackMessage struct {
	ChannelID        string
	ChannelName      string
	UserID           string
	UserName         string
	Text             string
	Timestamp        string
	ThreadTimestamp  string
	ParentUserID     string
	Permalink        string
	TeamID           string
	AppID            string
	BotID            string
	Language         string
	IsBot            bool
	IsThreadReply    bool
	EditedTimestamp  *time.Time
	EditedUserID     string
	ReplyCount       int
	ReplyUsers       []string
	Files            []SlackFile
	Reactions        []SlackReaction
	LastReadAt       *time.Time
	ThreadRootUserID string
}

// SlackFile captures metadata for a file that is attached to a Slack message.
type SlackFile struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Title              string `json:"title"`
	Filetype           string `json:"filetype"`
	URLPrivate         string `json:"url_private"`
	URLPrivateDownload string `json:"url_private_download"`
	Permalink          string `json:"permalink"`
	Size               int    `json:"size"`
}

// SlackReaction represents a reaction applied to a Slack message.
type SlackReaction struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	Users []string `json:"users"`
}

// FetchConfig defines configuration parameters when fetching Slack messages.
type FetchConfig struct {
	ChannelIDs       []string
	From             *time.Time
	To               *time.Time
	IncludeThreads   bool
	ExcludeBots      bool
	PageSize         int
	Limit            int
	MinMessageLength int
}

// ToVectorData converts the Slack message into a VectorData structure. The provided
// embedding slice may be nil when called prior to embedding generation.
func (m SlackMessage) ToVectorData(embedding []float64) types.VectorData {
	metadata := m.ToDocumentMetadata()

	return types.VectorData{
		ID:        m.GenerateVectorID(),
		Embedding: embedding,
		Metadata:  metadata,
		Content:   m.Text,
		CreatedAt: metadata.CreatedAt,
	}
}

// ToDocumentMetadata converts the Slack message into DocumentMetadata according to
// the project requirements.
func (m SlackMessage) ToDocumentMetadata() types.DocumentMetadata {
	channelName := m.ChannelName
	if channelName == "" {
		channelName = m.ChannelID
	}

	userName := m.UserName
	if userName == "" {
		userName = m.UserID
	}

	title := fmt.Sprintf("#%s - %s", channelName, userName)
	createdAt := m.EventTime()
	updatedAt := createdAt
	if m.EditedTimestamp != nil && !m.EditedTimestamp.IsZero() && m.EditedTimestamp.After(updatedAt) {
		updatedAt = *m.EditedTimestamp
	}

	tags := []string{"slack"}
	if channelName != "" {
		tags = append(tags, channelName)
	}

	customFields := map[string]interface{}{
		"channel_id":       m.ChannelID,
		"channel_name":     m.ChannelName,
		"user_id":          m.UserID,
		"user_name":        m.UserName,
		"thread_ts":        m.ThreadTimestamp,
		"message_ts":       m.Timestamp,
		"parent_user_id":   m.ParentUserID,
		"permalink":        m.Permalink,
		"team_id":          m.TeamID,
		"app_id":           m.AppID,
		"bot_id":           m.BotID,
		"is_bot":           m.IsBot,
		"is_thread_reply":  m.IsThreadReply,
		"reply_count":      m.ReplyCount,
		"reply_users":      m.ReplyUsers,
		"language":         m.Language,
		"edited_timestamp": m.editedTimestampString(),
		"edited_user_id":   m.EditedUserID,
		"files":            m.Files,
		"reactions":        m.Reactions,
		"last_read_at":     m.lastReadString(),
		"thread_root_user": m.ThreadRootUserID,
		"vector_source":    SourceSlack,
		"vector_category":  CategorySlackMessage,
	}

	return types.DocumentMetadata{
		Title:        title,
		Category:     CategorySlackMessage,
		Tags:         tags,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
		Author:       userName,
		Reference:    m.Permalink,
		Source:       SourceSlack,
		FilePath:     fmt.Sprintf("slack/%s/%s", m.ChannelID, m.Timestamp),
		WordCount:    m.wordCount(),
		CustomFields: customFields,
	}
}

// GenerateVectorID returns the deterministic identifier used for vector storage.
func (m SlackMessage) GenerateVectorID() string {
	return fmt.Sprintf("slack-%s-%s", m.ChannelID, m.Timestamp)
}

// EventTime converts the Slack timestamp into time.Time. Invalid timestamps return the zero time.
func (m SlackMessage) EventTime() time.Time {
	if m.Timestamp == "" {
		return time.Time{}
	}

	parts := strings.Split(m.Timestamp, ".")
	secondsPart := parts[0]
	sec, err := strconv.ParseInt(secondsPart, 10, 64)
	if err != nil {
		return time.Time{}
	}

	var nsec int64
	if len(parts) > 1 {
		// Slack uses microseconds in the fractional component.
		frac := parts[1]
		if len(frac) > 9 {
			frac = frac[:9]
		}
		for len(frac) < 9 {
			frac += "0"
		}

		nsec, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			nsec = 0
		}
	}

	return time.Unix(sec, nsec).UTC()
}

func (m SlackMessage) wordCount() int {
	if strings.TrimSpace(m.Text) == "" {
		return 0
	}

	return len(strings.Fields(m.Text))
}

func (m SlackMessage) editedTimestampString() string {
	if m.EditedTimestamp == nil || m.EditedTimestamp.IsZero() {
		return ""
	}
	return m.EditedTimestamp.UTC().Format(time.RFC3339Nano)
}

func (m SlackMessage) lastReadString() string {
	if m.LastReadAt == nil || m.LastReadAt.IsZero() {
		return ""
	}
	return m.LastReadAt.UTC().Format(time.RFC3339Nano)
}
