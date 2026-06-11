package slackbot

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/slack-go/slack/socketmode"
)

func TestSocketBotHandleEventDoesNotLogActionToken(t *testing.T) {
	const actionToken = "test-action-token-redacted"

	var logs bytes.Buffer
	bot := &SocketBot{logger: log.New(&logs, "", 0)}
	event := socketmode.Event{
		Type: socketmode.EventTypeConnecting,
		Data: map[string]string{
			"action_token": actionToken,
		},
	}

	stdout := captureStdout(t, func() {
		bot.handleEvent(context.Background(), event)
	})

	if strings.Contains(logs.String(), actionToken) {
		t.Fatalf("action token leaked to logger: %q", logs.String())
	}
	if strings.Contains(stdout, actionToken) {
		t.Fatalf("action token leaked to stdout: %q", stdout)
	}
	if !strings.Contains(logs.String(), "handleEvent type=") {
		t.Fatalf("expected type-only handleEvent log, got %q", logs.String())
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	defer func() {
		os.Stdout = original
	}()

	os.Stdout = writer
	fn()
	os.Stdout = original

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close stdout writer: %v", err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("failed to close stdout reader: %v", err)
	}
	return string(out)
}
