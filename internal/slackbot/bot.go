package slackbot

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/slack-go/slack"
)

// RTMClient abstracts slack.RTM for testability
type RTMClient interface {
	ManageConnection()
	IncomingEvents() chan slack.RTMEvent
	SendMessage(channel string, opts ...slack.MsgOption) (string, string, error)
	Disconnect() error
	Typing(channel string)
}

// SlackClient wraps a subset of slack.Client we rely on
type SlackClient interface {
	AuthTest() (*slack.AuthTestResponse, error)
	NewRTM(options ...slack.RTMOption) *slack.RTM
	PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
	GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
}

// Bot encapsulates RTM handling and message processing
type Bot struct {
	client        SlackClient
	rtm           RTMClient
	processor     *Processor
	logger        *log.Logger
	botUserID     string
	shutdownHooks []func()
	enableThread  bool
	rate          *RateLimiter
	metrics       Metrics
	reporter      ErrorReporter
}

// NewBot constructs a Slack Bot
func NewBot(client SlackClient, processor *Processor, logger *log.Logger) (*Bot, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "slackbot ", log.LstdFlags)
	}
	// auth test to get bot user id
	auth, err := client.AuthTest()
	if err != nil {
		return nil, err
	}
	bot := &Bot{
		client:    client,
		processor: processor,
		logger:    logger,
		botUserID: auth.UserID,
		reporter:  &noopReporter{},
	}
	// initialize RTM
	rtm := client.NewRTM()
	bot.rtm = &rtmWrapper{rtm: rtm, client: client}
	return bot, nil
}

// NewBotWithRTM allows injecting a custom RTM client and bot user id (for testing)
func NewBotWithRTM(client SlackClient, rtm RTMClient, processor *Processor, logger *log.Logger, botUserID string) *Bot {
	if logger == nil {
		logger = log.New(os.Stdout, "slackbot ", log.LstdFlags)
	}
	return &Bot{
		client:    client,
		rtm:       rtm,
		processor: processor,
		logger:    logger,
		botUserID: botUserID,
		reporter:  &noopReporter{},
	}
}

// SetRTM replaces the RTM client (useful for tests)
func (b *Bot) SetRTM(r RTMClient) { b.rtm = r }

// Start establishes the RTM connection and begins event loop.
func (b *Bot) Start(ctx context.Context) error {
	// handle graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		b.logger.Println("received shutdown signal")
		cancel()
	}()

	// run RTM connection management in background
	go b.rtm.ManageConnection()

	for {
		select {
		case <-ctx.Done():
			_ = b.rtm.Disconnect()
			for _, h := range b.shutdownHooks {
				h()
			}
			return nil
		case ev := <-b.rtm.IncomingEvents():
			b.handleEvent(ctx, ev)
		}
	}
}

func (b *Bot) handleEvent(ctx context.Context, ev slack.RTMEvent) {
	fmt.Printf("handleEvent: %+v\n", ev)

	switch data := ev.Data.(type) {
	case *slack.MessageEvent:
		if data.SubType != "" { // ignore bot_message, message_changed, etc.
			return
		}
		// gate: mention/DM + rate limit
		if !b.processor.IsMentionOrDM(b.botUserID, data) {
			return
		}
		if b.rate != nil && !b.rate.Allow(data.User, data.Channel) {
			b.logger.Printf("rate_limit_exceeded user=%s channel=%s", data.User, data.Channel)
			return
		}
		// typing indicator as progress
		b.rtm.Typing(data.Channel)
		// process
		b.metrics.RecordRequest()
		start := time.Now()
		reply := b.processor.ProcessMessage(ctx, b.botUserID, data)
		if reply == nil {
			return
		}
		// send reply
		for _, opt := range reply.MsgOptions {
			if b.enableThread {
				threadTS := data.ThreadTimestamp
				if threadTS == "" {
					threadTS = data.Timestamp
				}
				opt = slack.MsgOptionCompose(opt, slack.MsgOptionTS(threadTS))
			}
			_, _, err := b.client.PostMessage(reply.Channel, opt)
			if err != nil {
				b.metrics.RecordError()
				b.logger.Printf("event=post_message status=error err=%v", err)
				if b.reporter != nil {
					b.reporter.Report(err, map[string]string{"channel": reply.Channel})
				}
			} else {
				// slack rate limits; small sleep to be safe
				time.Sleep(50 * time.Millisecond)
			}
		}
		b.metrics.RecordResponse(time.Since(start))
	default:
		// other events ignored for now
	}
}

// rtM wrapper to satisfy RTMClient
type rtmWrapper struct {
	rtm    *slack.RTM
	client SlackClient
}

func (w *rtmWrapper) ManageConnection()                   { w.rtm.ManageConnection() }
func (w *rtmWrapper) IncomingEvents() chan slack.RTMEvent { return w.rtm.IncomingEvents }
func (w *rtmWrapper) Disconnect() error                   { return w.rtm.Disconnect() }
func (w *rtmWrapper) SendMessage(channel string, opts ...slack.MsgOption) (string, string, error) {
	return w.client.PostMessage(channel, opts...)
}
func (w *rtmWrapper) Typing(channel string) { w.rtm.SendMessage(w.rtm.NewTypingMessage(channel)) }

// Option setters
func (b *Bot) SetEnableThreading(v bool)      { b.enableThread = v }
func (b *Bot) SetRateLimiter(rl *RateLimiter) { b.rate = rl }
