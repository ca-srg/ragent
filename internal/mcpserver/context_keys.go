package mcpserver

type contextKey string

const (
	userContextKey       contextKey = "user"
	authMethodContextKey contextKey = "auth_method"
	clientIPContextKey   contextKey = "client_ip"
)
