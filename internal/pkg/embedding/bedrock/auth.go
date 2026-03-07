package bedrock

import (
	"context"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

type bearerTokenTransport struct {
	token     string
	transport http.RoundTripper
}

func (t *bearerTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)

	transport := t.transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	return transport.RoundTrip(clone)
}

func BuildBedrockAWSConfig(ctx context.Context, region, bearerToken string) (aws.Config, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}

	if bearerToken != "" {
		opts = append(opts,
			awsconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider("BEDROCK_BEARER", "BEDROCK_BEARER", ""),
			),
			awsconfig.WithHTTPClient(&http.Client{
				Transport: &bearerTokenTransport{token: bearerToken},
			}),
		)
	}

	return awsconfig.LoadDefaultConfig(ctx, opts...)
}
