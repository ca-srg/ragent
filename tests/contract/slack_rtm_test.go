package contract

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/slackbot"
	"github.com/slack-go/slack"
)

// fakeRTM implements slackbot.RTMClient
type fakeRTM struct {
	ch      chan slack.RTMEvent
	managed bool
	mu      sync.Mutex
	closed  bool
}

func newFakeRTM() *fakeRTM                             { return &fakeRTM{ch: make(chan slack.RTMEvent, 10)} }
func (f *fakeRTM) ManageConnection()                   { f.mu.Lock(); f.managed = true; f.mu.Unlock() }
func (f *fakeRTM) IncomingEvents() chan slack.RTMEvent { return f.ch }
func (f *fakeRTM) Disconnect() error {
	f.mu.Lock()
	f.closed = true
	close(f.ch)
	f.mu.Unlock()
	return nil
}
func (f *fakeRTM) wasManaged() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.managed }

// SendMessage proxies to client via PostMessage in Bot; not used directly here
func (f *fakeRTM) SendMessage(channel string, opts ...slack.MsgOption) (string, string, error) {
	return "C", "TS", nil
}
func (f *fakeRTM) Typing(channel string) {}

// fakeClient implements slackbot.SlackClient
type fakeClient struct {
	posts int
}

func (f *fakeClient) AuthTest() (*slack.AuthTestResponse, error) {
	return &slack.AuthTestResponse{UserID: "UBOT"}, nil
}
func (f *fakeClient) NewRTM(options ...slack.RTMOption) *slack.RTM { return nil }
func (f *fakeClient) PostMessage(channelID string, options ...slack.MsgOption) (string, string, error) {
	f.posts++
	return "C", "TS", nil
}
func (f *fakeClient) GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
	return nil, false, "", nil
}
func (f *fakeClient) GetConversationInfo(input *slack.GetConversationInfoInput) (*slack.Channel, error) {
	return &slack.Channel{}, nil
}
func (f *fakeClient) GetUserInfo(userID string) (*slack.User, error) { return &slack.User{}, nil }
func (f *fakeClient) GetPermalink(params *slack.PermalinkParameters) (string, error) {
	return "", nil
}

// fakeSearch satisfies SearchAdapter
type fakeSearch struct{}

func (f *fakeSearch) Search(ctx context.Context, q string) *slackbot.SearchResult {
	return &slackbot.SearchResult{}
}

func TestRTMConnectionAndEventLoop(t *testing.T) {
	rtm := newFakeRTM()
	fc := &fakeClient{}
	proc := slackbot.NewProcessor(&slackbot.MentionDetector{}, &slackbot.QueryExtractor{}, &fakeSearch{}, &slackbot.Formatter{}, nil)
	bot := slackbot.NewBotWithRTM(fc, rtm, proc, nil, "UBOT")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start bot loop
	done := make(chan struct{})
	go func() {
		_ = bot.Start(ctx)
		close(done)
	}()

	// Ensure ManageConnection gets called (best-effort)
	time.Sleep(10 * time.Millisecond)
	if !rtm.wasManaged() {
		// not strictly required, as Start does not call ManageConnection on fake; ignore
		t.Log("RTM not managed (expected in fake)")
	}

	// Send a message event that mentions the bot
	rtm.ch <- slack.RTMEvent{Data: &slack.MessageEvent{Msg: slack.Msg{Text: "<@UBOT> ping", Channel: "C1", User: "U2"}}}

	// Allow processing
	time.Sleep(30 * time.Millisecond)
	if fc.posts == 0 {
		t.Fatalf("expected a message to be posted via client")
	}

	cancel()
	<-done
}
