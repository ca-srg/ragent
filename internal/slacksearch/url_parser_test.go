package slacksearch

import (
	"testing"
)

func TestParseSlackURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		wantChannel string
		wantTS      string
		wantThread  string
		wantErr     bool
	}{
		{
			name:        "standard message URL",
			url:         "https://myworkspace.slack.com/archives/C01234567/p1234567890123456",
			wantChannel: "C01234567",
			wantTS:      "1234567890.123456",
			wantThread:  "",
			wantErr:     false,
		},
		{
			name:        "message URL with thread_ts",
			url:         "https://myworkspace.slack.com/archives/C01234567/p1234567890123456?thread_ts=1234567890.654321",
			wantChannel: "C01234567",
			wantTS:      "1234567890.123456",
			wantThread:  "1234567890.654321",
			wantErr:     false,
		},
		{
			name:        "app.slack.com URL",
			url:         "https://app.slack.com/archives/C98765432/p9876543210123456",
			wantChannel: "C98765432",
			wantTS:      "9876543210.123456",
			wantThread:  "",
			wantErr:     false,
		},
		{
			name:    "non-slack URL",
			url:     "https://example.com/archives/C01234567/p1234567890123456",
			wantErr: true,
		},
		{
			name:    "invalid format - missing p prefix timestamp",
			url:     "https://myworkspace.slack.com/archives/C01234567/1234567890123456",
			wantErr: true,
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
		},
		{
			name:    "invalid URL format",
			url:     "not-a-url",
			wantErr: true,
		},
		{
			name:        "URL with additional query params",
			url:         "https://myworkspace.slack.com/archives/C01234567/p1234567890123456?thread_ts=1234567890.654321&cid=C01234567",
			wantChannel: "C01234567",
			wantTS:      "1234567890.123456",
			wantThread:  "1234567890.654321",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseSlackURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseSlackURL() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseSlackURL() unexpected error: %v", err)
				return
			}

			if result.ChannelID != tt.wantChannel {
				t.Errorf("ParseSlackURL() ChannelID = %q, want %q", result.ChannelID, tt.wantChannel)
			}
			if result.MessageTS != tt.wantTS {
				t.Errorf("ParseSlackURL() MessageTS = %q, want %q", result.MessageTS, tt.wantTS)
			}
			if result.ThreadTS != tt.wantThread {
				t.Errorf("ParseSlackURL() ThreadTS = %q, want %q", result.ThreadTS, tt.wantThread)
			}
			if result.OriginalURL != tt.url {
				t.Errorf("ParseSlackURL() OriginalURL = %q, want %q", result.OriginalURL, tt.url)
			}
		})
	}
}

func TestDetectSlackURLs(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantCount int
		wantURLs  []string
	}{
		{
			name:      "single URL in text",
			text:      "Check this message: https://myworkspace.slack.com/archives/C01234567/p1234567890123456",
			wantCount: 1,
			wantURLs:  []string{"https://myworkspace.slack.com/archives/C01234567/p1234567890123456"},
		},
		{
			name:      "multiple URLs in text",
			text:      "See https://myworkspace.slack.com/archives/C01234567/p1234567890123456 and https://myworkspace.slack.com/archives/C98765432/p9876543210123456",
			wantCount: 2,
		},
		{
			name:      "URL with trailing punctuation",
			text:      "Check this: https://myworkspace.slack.com/archives/C01234567/p1234567890123456.",
			wantCount: 1,
		},
		{
			name:      "URL in markdown link",
			text:      "[link](https://myworkspace.slack.com/archives/C01234567/p1234567890123456)",
			wantCount: 1,
		},
		{
			name:      "no URLs",
			text:      "Just some text without any URLs",
			wantCount: 0,
		},
		{
			name:      "empty text",
			text:      "",
			wantCount: 0,
		},
		{
			name:      "duplicate URLs",
			text:      "https://myworkspace.slack.com/archives/C01234567/p1234567890123456 and https://myworkspace.slack.com/archives/C01234567/p1234567890123456",
			wantCount: 1, // Should deduplicate
		},
		{
			name:      "non-slack URLs ignored",
			text:      "Check https://example.com and https://myworkspace.slack.com/archives/C01234567/p1234567890123456",
			wantCount: 1, // Only Slack URL counted
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := DetectSlackURLs(tt.text)

			if len(results) != tt.wantCount {
				t.Errorf("DetectSlackURLs() returned %d URLs, want %d", len(results), tt.wantCount)
			}

			if tt.wantURLs != nil && len(results) > 0 {
				for i, url := range tt.wantURLs {
					if i < len(results) && results[i].OriginalURL != url {
						t.Errorf("DetectSlackURLs()[%d].OriginalURL = %q, want %q", i, results[i].OriginalURL, url)
					}
				}
			}
		})
	}
}

func TestHasSlackURL(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "has Slack URL",
			text: "Check https://myworkspace.slack.com/archives/C01234567/p1234567890123456",
			want: true,
		},
		{
			name: "no Slack URL",
			text: "Just some text",
			want: false,
		},
		{
			name: "empty text",
			text: "",
			want: false,
		},
		{
			name: "non-Slack URL",
			text: "https://example.com/path",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasSlackURL(tt.text); got != tt.want {
				t.Errorf("HasSlackURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertSlackTimestamp(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "with p prefix",
			input: "p1234567890123456",
			want:  "1234567890.123456",
		},
		{
			name:  "without p prefix",
			input: "1234567890123456",
			want:  "1234567890.123456",
		},
		{
			name:  "invalid length",
			input: "12345",
			want:  "12345", // Returns as-is
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertSlackTimestamp(tt.input); got != tt.want {
				t.Errorf("ConvertSlackTimestamp() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractQueryWithoutURLs(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "remove URL from query",
			text: "この問題について https://myworkspace.slack.com/archives/C01234567/p1234567890123456 を参考にしてください",
			want: "この問題について を参考にしてください",
		},
		{
			name: "multiple URLs removed",
			text: "参照: https://myworkspace.slack.com/archives/C01234567/p1234567890123456 と https://myworkspace.slack.com/archives/C98765432/p9876543210123456",
			want: "参照: と",
		},
		{
			name: "no URLs",
			text: "質問があります",
			want: "質問があります",
		},
		{
			name: "only URL",
			text: "https://myworkspace.slack.com/archives/C01234567/p1234567890123456",
			want: "",
		},
		{
			name: "empty",
			text: "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractQueryWithoutURLs(tt.text); got != tt.want {
				t.Errorf("ExtractQueryWithoutURLs() = %q, want %q", got, tt.want)
			}
		})
	}
}
