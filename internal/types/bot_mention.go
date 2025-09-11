package types

// BotMention captures mention context
type BotMention struct {
	BotUserID string
	Channel   string
	User      string
	Text      string
	IsThread  bool
}
