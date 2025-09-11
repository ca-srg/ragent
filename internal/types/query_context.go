package types

// QueryContext represents extracted query and metadata
type QueryContext struct {
	Query      string
	Channel    string
	User       string
	ThreadTS   string
	MaxResults int
}
