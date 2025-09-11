package types

// SlackMessage represents a simplified Slack message
type SlackMessage struct {
	Channel   string
	User      string
	Text      string
	Timestamp string
	ThreadTS  string
}
