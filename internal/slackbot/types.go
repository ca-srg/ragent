package slackbot

// BotMention captures mention context
type BotMention struct {
	BotUserID string
	Channel   string
	User      string
	Text      string
	IsThread  bool
}

// QueryContext represents extracted query and metadata
type QueryContext struct {
	Query      string
	Channel    string
	User       string
	ThreadTS   string
	MaxResults int
}

// SlackMessage represents a simplified Slack message
type SlackMessage struct {
	Channel   string
	User      string
	Text      string
	Timestamp string
	ThreadTS  string
}

// SlackReply holds message data to send back
type SlackReply struct {
	Channel string
	Text    string
	Blocks  interface{}
}
