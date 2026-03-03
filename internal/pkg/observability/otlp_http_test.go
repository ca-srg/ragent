package observability

import "testing"

func TestNormalizeOTLPHTTPPath(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		name     string
		endpoint string
		suffix   string
		want     string
		wantErr  bool
	}{
		{
			name:     "no path appends suffix",
			endpoint: "https://collector:4318",
			suffix:   "/v1/metrics",
			want:     "https://collector:4318/v1/metrics",
		},
		{
			name:     "http scheme preserved",
			endpoint: "http://localhost:4318",
			suffix:   "/v1/traces",
			want:     "http://localhost:4318/v1/traces",
		},
		{
			name:     "otlp prefix gets metrics suffix",
			endpoint: "https://example.com/otlp",
			suffix:   "/v1/metrics",
			want:     "https://example.com/otlp/v1/metrics",
		},
		{
			name:     "trailing slash ignored",
			endpoint: "https://example.com/otlp/",
			suffix:   "/v1/metrics",
			want:     "https://example.com/otlp/v1/metrics",
		},
		{
			name:     "suffix already present",
			endpoint: "https://example.com/otlp/v1/metrics",
			suffix:   "/v1/metrics",
			want:     "https://example.com/otlp/v1/metrics",
		},
		{
			name:     "query string preserved",
			endpoint: "https://example.com/otlp?token=abc",
			suffix:   "/v1/traces",
			want:     "https://example.com/otlp/v1/traces?token=abc",
		},
		{
			name:     "empty endpoint error",
			endpoint: "",
			suffix:   "/v1/metrics",
			wantErr:  true,
		},
	}

	for _, tt := range testcases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeOTLPHTTPPath(tt.endpoint, tt.suffix)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
