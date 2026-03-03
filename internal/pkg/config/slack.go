package config

import (
	"time"

	env "github.com/netflix/go-env"
)

// SlackConfig holds Slack-related settings
type SlackConfig struct {
	BotToken  string `env:"SLACK_BOT_TOKEN,required=true"`
	UserToken string `env:"SLACK_USER_TOKEN,required=false"`
	// App-level token for Socket Mode (xapp-)
	AppToken string `env:"SLACK_APP_TOKEN,required=false"`
	// Enable Socket Mode when true (requires AppToken)
	SocketMode      bool          `env:"SLACK_SOCKET_MODE,default=false"`
	ResponseTimeout time.Duration `env:"SLACK_RESPONSE_TIMEOUT,default=5s"`
	MaxResults      int           `env:"SLACK_MAX_RESULTS,default=5"`
	EnableThreading bool          `env:"SLACK_ENABLE_THREADING,default=true"`
	// Thread context settings
	ThreadContextEnabled     bool `env:"SLACK_THREAD_CONTEXT_ENABLED,default=true"`
	ThreadContextMaxMessages int  `env:"SLACK_THREAD_CONTEXT_MAX_MESSAGES,default=10"`
	// Rate limits per minute
	RateUserPerMinute    int `env:"SLACK_RATE_USER_PER_MINUTE,default=10"`
	RateChannelPerMinute int `env:"SLACK_RATE_CHANNEL_PER_MINUTE,default=30"`
	RateGlobalPerMinute  int `env:"SLACK_RATE_GLOBAL_PER_MINUTE,default=100"`
}

// LoadSlack loads Slack configuration from environment variables
func LoadSlack() (*SlackConfig, error) {
	var cfg SlackConfig
	_, err := env.UnmarshalFromEnviron(&cfg)
	if err != nil {
		return nil, err
	}
	// If AppToken is present but SocketMode is not explicitly set, enable Socket Mode automatically
	if cfg.AppToken != "" && !cfg.SocketMode {
		cfg.SocketMode = true
	}
	if cfg.MaxResults <= 0 {
		cfg.MaxResults = 5
	}
	if cfg.ThreadContextMaxMessages <= 0 {
		cfg.ThreadContextMaxMessages = 10
	}
	return &cfg, nil
}
