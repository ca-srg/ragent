package slackbot

import "context"

// actionTokenContextKey is the unexported context key used to propagate the
// Slack Real-time Search API `action_token` from the event-receive layer
// (Socket Mode) down to the search adapter, without having to widen the
// Processor.ProcessMessage signature.
type actionTokenContextKey struct{}

// ContextWithActionToken stores a Slack `action_token` on ctx. The token is
// surfaced by `app_mention` / `message` event payloads under
// `event.assistant_thread.action_token` and is required to call
// `assistant.search.context` with a bot token. An empty token is a no-op.
func ContextWithActionToken(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return context.WithValue(ctx, actionTokenContextKey{}, token)
}

// ActionTokenFromContext returns the action_token stored on ctx, or "" if
// none is present.
func ActionTokenFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	token, _ := ctx.Value(actionTokenContextKey{}).(string)
	return token
}
