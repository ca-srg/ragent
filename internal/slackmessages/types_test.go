package slackmessages

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlackMessage_ToDocumentMetadata(t *testing.T) {
	editedSecond := time.Unix(1700000500, 0).Add(90 * time.Second).UTC()
	testCases := []struct {
		name           string
		message        SlackMessage
		wantTitle      string
		wantTags       []string
		wantAuthor     string
		wantFilePath   string
		wantWordCount  int
		wantCreatedUTC time.Time
		wantUpdatedUTC time.Time
	}{
		{
			name: "standard message with channel and user names",
			message: SlackMessage{
				ChannelID:       "C123",
				ChannelName:     "general",
				UserID:          "U123",
				UserName:        "john.doe",
				Text:            "Hello world from Slack",
				Timestamp:       "1700000000.123456",
				ThreadTimestamp: "1700000000.123456",
				Permalink:       "https://slack.example.com/archives/C123/p1700000000123456",
				ReplyCount:      2,
				ReplyUsers:      []string{"U999"},
				BotID:           "",
			},
			wantTitle:      "#general - john.doe",
			wantTags:       []string{"slack", "general"},
			wantAuthor:     "john.doe",
			wantFilePath:   "slack/C123/1700000000.123456",
			wantWordCount:  4,
			wantCreatedUTC: time.Unix(1700000000, 123456000).UTC(),
			wantUpdatedUTC: time.Unix(1700000000, 123456000).UTC(),
		},
		{
			name: "fallbacks when channel and user names missing",
			message: SlackMessage{
				ChannelID:       "C456",
				UserID:          "U789",
				Text:            "Edge case text to evaluate metadata",
				Timestamp:       "1700000500",
				ThreadTimestamp: "",
				EditedTimestamp: &editedSecond,
				IsBot:           true,
				BotID:           "B42",
			},
			wantTitle:      "#C456 - U789",
			wantTags:       []string{"slack", "C456"},
			wantAuthor:     "U789",
			wantFilePath:   "slack/C456/1700000500",
			wantWordCount:  6,
			wantCreatedUTC: time.Unix(1700000500, 0).UTC(),
			wantUpdatedUTC: editedSecond,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			meta := tc.message.ToDocumentMetadata()

			require.Equal(t, tc.wantTitle, meta.Title)
			require.Equal(t, CategorySlackMessage, meta.Category)
			require.Equal(t, tc.wantTags, meta.Tags)
			require.Equal(t, tc.wantAuthor, meta.Author)
			require.Equal(t, SourceSlack, meta.Source)
			require.Equal(t, tc.wantFilePath, meta.FilePath)
			require.Equal(t, tc.wantWordCount, meta.WordCount)
			require.WithinDuration(t, tc.wantCreatedUTC, meta.CreatedAt, time.Millisecond)
			require.WithinDuration(t, tc.wantUpdatedUTC, meta.UpdatedAt, time.Millisecond)

			assert.Equal(t, tc.message.Permalink, meta.Reference)

			fields := meta.CustomFields
			require.NotNil(t, fields)
			assert.Equal(t, tc.message.ChannelID, fields["channel_id"])
			assert.Equal(t, tc.message.ChannelName, fields["channel_name"])
			assert.Equal(t, tc.message.UserID, fields["user_id"])
			assert.Equal(t, tc.message.UserName, fields["user_name"])
			assert.Equal(t, tc.message.ThreadTimestamp, fields["thread_ts"])
			assert.Equal(t, tc.message.Timestamp, fields["message_ts"])
			assert.Equal(t, tc.message.ParentUserID, fields["parent_user_id"])
			assert.Equal(t, tc.message.Permalink, fields["permalink"])
			assert.Equal(t, tc.message.TeamID, fields["team_id"])
			assert.Equal(t, tc.message.BotID, fields["bot_id"])
			assert.Equal(t, tc.message.IsBot, fields["is_bot"])
			assert.Equal(t, SourceSlack, fields["vector_source"])
			assert.Equal(t, CategorySlackMessage, fields["vector_category"])

			if tc.message.EditedTimestamp != nil {
				assert.Equal(t, tc.message.EditedTimestamp.UTC().Format(time.RFC3339Nano), fields["edited_timestamp"])
			} else {
				assert.Equal(t, "", fields["edited_timestamp"])
			}
		})
	}
}

func TestSlackMessage_ToVectorData(t *testing.T) {
	edited := time.Unix(1700002000, 0).UTC()
	msg := SlackMessage{
		ChannelID:       "C321",
		ChannelName:     "random",
		UserID:          "U654",
		UserName:        "alice",
		Text:            "Vector conversion content",
		Timestamp:       "1700002000.500000",
		ThreadTimestamp: "1700002000.500000",
		Permalink:       "https://slack.example.com/archives/C321/p1700002000500000",
		ReplyCount:      1,
		ReplyUsers:      []string{"U777"},
		EditedTimestamp: &edited,
	}

	meta := msg.ToDocumentMetadata()
	embedding := []float64{0.1, 0.2, 0.3}
	vector := msg.ToVectorData(embedding)

	require.Equal(t, msg.GenerateVectorID(), vector.ID)
	require.Equal(t, embedding, vector.Embedding)
	require.Equal(t, msg.Text, vector.Content)
	require.WithinDuration(t, meta.CreatedAt, vector.CreatedAt, time.Millisecond)
	require.Equal(t, meta, vector.Metadata)
}

func TestSlackMessage_GenerateVectorID(t *testing.T) {
	msg := SlackMessage{
		ChannelID: "C999",
		Timestamp: "1700003000.765432",
	}
	require.Equal(t, "slack-C999-1700003000.765432", msg.GenerateVectorID())

	msgEmpty := SlackMessage{}
	require.Equal(t, "slack--", msgEmpty.GenerateVectorID())
}
