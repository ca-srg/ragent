package slackbot

import (
	"context"
	"fmt"
	"log"
	"os"
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
	enableThread bool
	rate         *RateLimiter
	metrics      Metrics
	reporter     ErrorReporter
	vectorize    *vectorizeSupport
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
		reporter:  &noopReporter{},
		vectorize: newVectorizeSupport(client, logger),
	}, nil
}

// Option setters
func (b *SocketBot) SetEnableThreading(v bool)      { b.enableThread = v }
func (b *SocketBot) SetRateLimiter(rl *RateLimiter) { b.rate = rl }
func (b *SocketBot) SetVectorizer(vectorizer realtimeVectorizer, opts VectorizeOptions) {
	b.vectorize.configure(vectorizer, opts)
}

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
		// Ack first to avoid retries
		if ev.Request != nil {
			b.sm.Ack(*ev.Request)
		}
		payload, ok := ev.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		if payload.Type != slackevents.CallbackEvent {
			return
		}
		inner := payload.InnerEvent
		switch data := inner.Data.(type) {
		case *slackevents.AppMentionEvent:
			msg := &slack.MessageEvent{
				Msg: slack.Msg{
					Channel:         data.Channel,
					User:            data.User,
					Text:            data.Text,
					Timestamp:       data.TimeStamp,
					ThreadTimestamp: data.ThreadTimeStamp,
				},
			}
			b.processMessage(ctx, msg)
		case *slackevents.MessageEvent:
			// Only process user messages
			if data.SubType != "" && data.SubType != slack.MsgSubTypeFileShare {
				return
			}
			msg := &slack.MessageEvent{
				Msg: slack.Msg{
					Channel:         data.Channel,
					User:            data.User,
					Text:            data.Text,
					Timestamp:       data.TimeStamp,
					ThreadTimestamp: data.ThreadTimeStamp,
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

func (b *SocketBot) processMessage(ctx context.Context, msg *slack.MessageEvent) {
	shouldRespond := b.processor.IsMentionOrDM(b.botUserID, msg)
	if b.vectorize.shouldVectorize(b.botUserID, msg) {
		msgCopy := *msg
		go b.vectorize.vectorize(&msgCopy)
	}
	if !shouldRespond {
		return
	}
	// rate limit per user/channel/global if configured
	if b.rate != nil && !b.rate.Allow(msg.User, msg.Channel) {
		b.logger.Printf("rate_limit_exceeded user=%s channel=%s", msg.User, msg.Channel)
		return
	}
	b.metrics.RecordRequest()
	start := time.Now()
	reply := b.processor.ProcessMessage(ctx, b.botUserID, msg)
	if reply == nil {
		return
	}
	for _, opt := range reply.MsgOptions {
		// Always reply in thread. Use ThreadTimestamp if exists, otherwise use Timestamp to start a new thread
		threadTS := msg.ThreadTimestamp
		if threadTS == "" {
			threadTS = msg.Timestamp
		}
		opt = slack.MsgOptionCompose(opt, slack.MsgOptionTS(threadTS))
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
