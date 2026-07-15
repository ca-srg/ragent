package mcpserver

import (
	"io"
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appcfg "github.com/ca-srg/ragent/internal/pkg/config"
)

func TestNewMCPServerSlackURLFetcher(t *testing.T) {
	logger := log.New(io.Discard, "", 0)

	client, fetcher := newMCPServerSlackURLFetcher(nil, 10, logger)
	assert.Nil(t, client)
	assert.Nil(t, fetcher)

	client, fetcher = newMCPServerSlackURLFetcher(&appcfg.SlackConfig{
		BotToken:             "xoxb-test",
		RateUserPerMinute:    10,
		RateChannelPerMinute: 30,
		RateGlobalPerMinute:  100,
	}, maxSlackURLFetchTimeoutSeconds+1, logger)
	require.NotNil(t, client)
	require.NotNil(t, fetcher)
}

func TestSlackURLFetchTokenPrefersUserToken(t *testing.T) {
	tests := []struct {
		name      string
		userToken string
		want      string
	}{
		{name: "user token", userToken: "xoxp-test", want: "xoxp-test"},
		{name: "missing user token", want: "xoxb-test"},
		{name: "blank user token", userToken: "  ", want: "xoxb-test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &appcfg.SlackConfig{BotToken: "xoxb-test", UserToken: tt.userToken}

			assert.Equal(t, tt.want, slackURLFetchToken(cfg))
		})
	}
}

func TestNormalizeSlackURLFetchTimeout(t *testing.T) {
	tests := map[int]int{
		-1: defaultSlackURLFetchTimeoutSeconds,
		0:  defaultSlackURLFetchTimeoutSeconds,
		1:  1,
		60: 60,
		61: defaultSlackURLFetchTimeoutSeconds,
	}
	for input, want := range tests {
		assert.Equal(t, want, normalizeSlackURLFetchTimeout(input))
	}
}
