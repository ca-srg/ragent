package slackbot

import (
	"context"
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
			b.sm.Ack(*ev.Request)
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
		if cb, ok := payload.Data.(*slackevents.EventsAPICallbackEvent); ok && cb != nil {
			eventID = cb.EventID
		}
		if eventID != "" {
			if b.dedup.markSeen(eventID) {
				b.logger.Printf("event=events_api status=duplicate event_id=%s", eventID)
				return
			}
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
			b.processMessage(ctx, msg)
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
			b.processMessage(ctx, msg)
		default:
			// ignore other events
		}
	default:
		// ignore
	}
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
