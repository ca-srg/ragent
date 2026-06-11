package slackbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// SocketBot handles Slack events via Socket Mode (xapp- token)
type SocketBot struct {
	client       *slack.Client
	sm           *socketmode.Client
	processor    *Processor
	logger       *log.Logger
	botUserID    string
	botID        string
	enableThread bool
	rate         *RateLimiter
	metrics      Metrics
	reporter     ErrorReporter
	dedup        *eventDedup
}

// NewSocketBot constructs a Socket Mode Slack Bot
func NewSocketBot(client *slack.Client, appToken string, processor *Processor, logger *log.Logger) (*SocketBot, error) {
	if client == nil {
		return nil, fmt.Errorf("nil slack client")
	}
	if appToken == "" {
		return nil, fmt.Errorf("app token required for socket mode")
	}
	if logger == nil {
		logger = log.New(os.Stdout, "slackbot ", log.LstdFlags)
	}
	// verify auth to obtain bot user id
	auth, err := client.AuthTest()
	if err != nil {
		return nil, err
	}
	// The app-level token must be set on the underlying slack.Client via slack.OptionAppLevelToken.
	// Here we assume caller passed a client constructed with that option.
	sm := socketmode.New(client)
	return &SocketBot{
		client:    client,
		sm:        sm,
		processor: processor,
		logger:    logger,
		botUserID: auth.UserID,
		botID:     auth.BotID,
		reporter:  &noopReporter{},
		dedup:     newEventDedup(5*time.Minute, 4096),
	}, nil
}

// Option setters
func (b *SocketBot) SetEnableThreading(v bool)      { b.enableThread = v }
func (b *SocketBot) SetRateLimiter(rl *RateLimiter) { b.rate = rl }

// Start begins the Socket Mode event loop
func (b *SocketBot) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Run websocket connection in background
	go func() {
		if err := b.sm.RunContext(ctx); err != nil {
			b.logger.Printf("socketmode run error: %v", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-b.sm.Events:
			b.handleEvent(ctx, ev)
		}
	}
}

func (b *SocketBot) handleEvent(ctx context.Context, ev socketmode.Event) {
	// lightweight diagnostics similar to RTM path
	fmt.Printf("handleEvent: {Type:%s Data:%v}\n", ev.Type, ev.Data)

	switch ev.Type {
	case socketmode.EventTypeConnecting:
		// just log
	case socketmode.EventTypeConnected:
		// connected
	case socketmode.EventTypeInvalidAuth:
		b.logger.Printf("invalid_auth: verify SLACK_APP_TOKEN and SLACK_BOT_TOKEN")
	case socketmode.EventTypeConnectionError:
		b.logger.Printf("connection_error: %v", ev.Data)
	case socketmode.EventTypeIncomingError:
		b.logger.Printf("incoming_error: %v", ev.Data)
	case socketmode.EventTypeEventsAPI:
		// Ack first to avoid Slack retries.
		if ev.Request != nil {
			if err := b.sm.Ack(*ev.Request); err != nil {
				b.logger.Printf("event=events_api ack_error=%v envelope_id=%s", err, ev.Request.EnvelopeID)
			}
			if ev.Request.RetryAttempt > 0 {
				b.logger.Printf("event=events_api retry_attempt=%d retry_reason=%s envelope_id=%s",
					ev.Request.RetryAttempt, ev.Request.RetryReason, ev.Request.EnvelopeID)
			}
		}
		payload, ok := ev.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		if payload.Type != slackevents.CallbackEvent {
			return
		}

		// Dedup: Slack may redeliver the same event_id when an Ack is not
		// observed in time, when the websocket reconnects, or when multiple
		// event subscriptions overlap. Process each event_id only once.
		var eventID string
		var rawInnerEvent json.RawMessage
		if cb, ok := payload.Data.(*slackevents.EventsAPICallbackEvent); ok && cb != nil {
			eventID = cb.EventID
			if cb.InnerEvent != nil {
				rawInnerEvent = *cb.InnerEvent
			}
		}
		if eventID != "" {
			if b.dedup.markSeen(eventID) {
				b.logger.Printf("event=events_api status=duplicate event_id=%s", eventID)
				return
			}
		}

		// Probe the raw inner event JSON for action_token because the public
		// docs and JS examples (https://docs.slack.dev/ai/developing-agents)
		// surface `event.action_token` at the top level, while slack-go
		// v0.23.1 only models the nested `event.assistant_thread.action_token`
		// path. We log both so the operator can see exactly which shape Slack
		// is sending in their workspace, and fall back to the top-level value
		// when slack-go fails to populate AssistantThread.
		probeToken, probeHasNested, probeHasTop := probeActionToken(rawInnerEvent)
		if len(rawInnerEvent) > 0 {
			b.logger.Printf("event=events_api action_token_probe top_level=%t assistant_thread=%t",
				probeHasTop, probeHasNested)
		}

		inner := payload.InnerEvent
		switch data := inner.Data.(type) {
		case *slackevents.AppMentionEvent:
			// Defense-in-depth: never react to events authored by a bot
			// (including ourselves) so a stray mention echoed in our own
			// reply cannot retrigger us.
			if b.isBotOrigin(data.BotID, data.User) {
				return
			}
			// Fallback dedup on channel:ts when event_id is missing.
			if eventID == "" {
				if b.dedup.markSeen("am:" + data.Channel + ":" + data.TimeStamp) {
					return
				}
			}
			msg := &slack.MessageEvent{
				Msg: slack.Msg{
					Channel:         data.Channel,
					User:            data.User,
					Text:            data.Text,
					Timestamp:       data.TimeStamp,
					ThreadTimestamp: data.ThreadTimeStamp,
					BotID:           data.BotID,
				},
			}
			// Surface the short-lived action_token (Real-time Search API) so
			// the search adapter can use assistant.search.context on a bot
			// token, which lets the bot reach public channels it is not in.
			msgCtx := ctx
			token := ""
			if data.AssistantThread != nil && data.AssistantThread.ActionToken != "" {
				token = data.AssistantThread.ActionToken
			}
			if token == "" {
				token = probeToken
			}
			if token != "" {
				msgCtx = ContextWithActionToken(msgCtx, token)
			}
			b.processMessage(msgCtx, msg)
		case *slackevents.MessageEvent:
			// Only process plain user messages.
			if data.SubType != "" { // ignore bot_message, message_changed, etc.
				return
			}
			// Ignore non-DM message events to avoid duplicates with AppMentionEvent.
			if !strings.HasPrefix(data.Channel, "D") {
				return
			}
			if b.isBotOrigin(data.BotID, data.User) {
				return
			}
			if eventID == "" {
				if b.dedup.markSeen("msg:" + data.Channel + ":" + data.TimeStamp) {
					return
				}
			}

			msg := &slack.MessageEvent{
				Msg: slack.Msg{
					Channel:         data.Channel,
					User:            data.User,
					Text:            data.Text,
					Timestamp:       data.TimeStamp,
					ThreadTimestamp: data.ThreadTimeStamp,
					BotID:           data.BotID,
				},
			}
			msgCtx := ctx
			token := ""
			if data.AssistantThread != nil && data.AssistantThread.ActionToken != "" {
				token = data.AssistantThread.ActionToken
			}
			if token == "" {
				token = probeToken
			}
			if token != "" {
				msgCtx = ContextWithActionToken(msgCtx, token)
			}
			b.processMessage(msgCtx, msg)
		default:
			// ignore other events
		}
	default:
		// ignore
	}
}

// probeActionToken extracts the Slack Real-time Search API action_token from
// the raw inner event JSON. Slack publishes it either at top-level
// (`event.action_token`, used by some official examples) or nested under
// `event.assistant_thread.action_token` (the shape slack-go models on the
// AppMentionEvent / MessageEvent structs). Both shapes are accepted so we
// can hand the token to assistant.search.context regardless of which form
// the workspace happens to deliver.
func probeActionToken(raw json.RawMessage) (token string, hasNested, hasTop bool) {
	if len(raw) == 0 {
		return "", false, false
	}
	var probe struct {
		ActionToken     string `json:"action_token"`
		AssistantThread *struct {
			ActionToken string `json:"action_token"`
		} `json:"assistant_thread"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "", false, false
	}
	if probe.AssistantThread != nil {
		hasNested = true
		if strings.TrimSpace(probe.AssistantThread.ActionToken) != "" {
			token = probe.AssistantThread.ActionToken
		}
	}
	if strings.TrimSpace(probe.ActionToken) != "" {
		hasTop = true
		if token == "" {
			token = probe.ActionToken
		}
	}
	return token, hasNested, hasTop
}

// isBotOrigin reports whether an inbound event was authored by a bot
// (including ourselves). We must skip such events to avoid responding to our
// own replies or to other bots' messages.
func (b *SocketBot) isBotOrigin(botID, userID string) bool {
	if botID != "" {
		return true
	}
	if userID == "" {
		return true
	}
	if userID == b.botUserID {
		return true
	}
	if b.botID != "" && botID == b.botID {
		return true
	}
	return false
}

func (b *SocketBot) processMessage(ctx context.Context, msg *slack.MessageEvent) {
	// rate limit per user/channel/global if configured
	if b.rate != nil && !b.rate.Allow(msg.User, msg.Channel) {
		b.logger.Printf("rate_limit_exceeded user=%s channel=%s", msg.User, msg.Channel)
		return
	}

	threadTS := msg.ThreadTimestamp
	if threadTS == "" {
		threadTS = msg.Timestamp
	}
	notifier := NewSlackProgressNotifier(b.client, msg.Channel, threadTS)
	ctx = ContextWithProgressNotifier(ctx, notifier)

	b.metrics.RecordRequest()
	start := time.Now()
	reply := b.processor.ProcessMessage(ctx, b.botUserID, msg)
	if reply == nil {
		return
	}
	for _, opt := range reply.MsgOptions {
		if b.enableThread {
			opt = slack.MsgOptionCompose(opt, slack.MsgOptionTS(threadTS))
		}
		if _, _, err := b.client.PostMessage(reply.Channel, opt); err != nil {
			b.metrics.RecordError()
			b.logger.Printf("event=post_message status=error err=%v", err)
			if b.reporter != nil {
				b.reporter.Report(err, map[string]string{"channel": reply.Channel})
			}
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	}
	b.metrics.RecordResponse(time.Since(start))
}
