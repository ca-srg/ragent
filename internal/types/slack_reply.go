package types

// SlackReply holds message data to send back
type SlackReply struct {
	Channel string
	Text    string
	Blocks  interface{}
}
