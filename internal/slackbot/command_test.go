package slackbot

import (
	"testing"

	appcfg "github.com/ca-srg/ragent/internal/pkg/config"
)

func TestValidateSlackSearchTokenConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		userToken string
		scfg      *appcfg.SlackConfig
		wantErr   bool
	}{
		{
			name:      "bot token only without socket mode returns error",
			userToken: "",
			scfg:      &appcfg.SlackConfig{SocketMode: false, AppToken: ""},
			wantErr:   true,
		},
		{
			name:      "bot token only with socket mode and app token succeeds",
			userToken: "",
			scfg:      &appcfg.SlackConfig{SocketMode: true, AppToken: "xapp-test-token"},
			wantErr:   false,
		},
		{
			name:      "bot token only with socket mode but blank app token returns error",
			userToken: "",
			scfg:      &appcfg.SlackConfig{SocketMode: true, AppToken: "   "},
			wantErr:   true,
		},
		{
			name:      "user token with RTM succeeds",
			userToken: "xoxp-test-token",
			scfg:      &appcfg.SlackConfig{SocketMode: false, AppToken: ""},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateSlackSearchTokenConfig(tt.userToken, tt.scfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected validation error")
				}
				if err.Error() != slackSearchTokenConfigError {
					t.Fatalf("unexpected error message: %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no validation error, got %v", err)
			}
		})
	}
}
