package config

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSMClient is a simple mock for SecretsManagerClient.
// It uses struct fields instead of testify/mock to keep dependencies minimal.
type mockSMClient struct {
	secretString *string // nil means SecretString is absent in the response
	returnErr    error
	callCount    int
}

func (m *mockSMClient) GetSecretValue(
	ctx context.Context,
	params *secretsmanager.GetSecretValueInput,
	optFns ...func(*secretsmanager.Options),
) (*secretsmanager.GetSecretValueOutput, error) {
	m.callCount++
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	return &secretsmanager.GetSecretValueOutput{
		SecretString: m.secretString,
	}, nil
}

// newMockWithSecret returns a mockSMClient whose SecretString is the given JSON.
func newMockWithSecret(jsonStr string) *mockSMClient {
	return &mockSMClient{secretString: aws.String(jsonStr)}
}

// setupMockFactory replaces smClientFactory with a function returning the given mock.
func setupMockFactory(mock *mockSMClient) {
	smClientFactory = func(ctx context.Context, region string) (SecretsManagerClient, error) {
		return mock, nil
	}
}

// setupMockFactoryWithRegionCapture replaces smClientFactory and captures
// the region argument passed by the production code, for verification.
func setupMockFactoryWithRegionCapture(mock *mockSMClient, capturedRegion *string) {
	smClientFactory = func(ctx context.Context, region string) (SecretsManagerClient, error) {
		*capturedRegion = region
		return mock, nil
	}
}

// unsetEnv removes an environment variable and registers t.Cleanup to restore it.
// Use this instead of t.Setenv("", "") when the test requires the variable to be
// truly absent from the environment (not just set to empty string).
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	prev, wasSet := os.LookupEnv(key)
	require.NoError(t, os.Unsetenv(key))
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// --- Core unit tests for LoadSecretsIntoEnv ---

// TestLoadSecretsIntoEnv_SecretIDNotSet_Noop verifies that when
// SECRET_MANAGER_SECRET_ID is not set, LoadSecretsIntoEnv does nothing.
func TestLoadSecretsIntoEnv_SecretIDNotSet_Noop(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	unsetEnv(t, envSecretManagerSecretID)

	called := false
	smClientFactory = func(ctx context.Context, region string) (SecretsManagerClient, error) {
		called = true
		return nil, nil
	}

	err := LoadSecretsIntoEnv(context.Background())
	require.NoError(t, err)
	assert.False(t, called, "smClientFactory must not be called when SECRET_MANAGER_SECRET_ID is not set")
}

// TestLoadSecretsIntoEnv_FallbackInjectsUnsetVars verifies the core fallback
// behavior: when an env var is truly unset, SM values are injected.
func TestLoadSecretsIntoEnv_FallbackInjectsUnsetVars(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	// Truly remove the variables from the environment so SM fallback kicks in.
	unsetEnv(t, "TEST_SM_INJECT_A")
	unsetEnv(t, "TEST_SM_INJECT_B")

	mock := newMockWithSecret(`{"TEST_SM_INJECT_A":"value-a","TEST_SM_INJECT_B":"value-b"}`)
	setupMockFactory(mock)

	err := LoadSecretsIntoEnv(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "value-a", os.Getenv("TEST_SM_INJECT_A"))
	assert.Equal(t, "value-b", os.Getenv("TEST_SM_INJECT_B"))
	assert.Equal(t, 1, mock.callCount, "SM should be called exactly once")
}

// TestLoadSecretsIntoEnv_ExistingEnvNotOverwritten verifies that env vars
// already set in the environment are NOT overwritten by SM values.
func TestLoadSecretsIntoEnv_ExistingEnvNotOverwritten(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	t.Setenv("EXISTING_VAR_SM", "original-value")
	unsetEnv(t, "NEW_VAR_SM")

	mock := newMockWithSecret(`{"EXISTING_VAR_SM":"sm-overwrite-attempt","NEW_VAR_SM":"new-value"}`)
	setupMockFactory(mock)

	err := LoadSecretsIntoEnv(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "original-value", os.Getenv("EXISTING_VAR_SM"),
		"existing env var must not be overwritten by Secrets Manager")
	assert.Equal(t, "new-value", os.Getenv("NEW_VAR_SM"),
		"unset var should be injected from Secrets Manager")
}

// TestLoadSecretsIntoEnv_SMError_ReturnsFatal verifies that when the SM client
// returns an error, LoadSecretsIntoEnv propagates it as a non-nil error.
func TestLoadSecretsIntoEnv_SMError_ReturnsFatal(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "bad-secret-id")

	mock := &mockSMClient{returnErr: errors.New("access denied by IAM policy")}
	setupMockFactory(mock)

	err := LoadSecretsIntoEnv(context.Background())
	require.Error(t, err, "SM client error must be propagated")
	assert.Contains(t, err.Error(), "access denied")
}

// TestLoadSecretsIntoEnv_InvalidJSON_ReturnsFatal verifies that malformed JSON
// in the secret string causes LoadSecretsIntoEnv to return an error.
func TestLoadSecretsIntoEnv_InvalidJSON_ReturnsFatal(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")

	mock := newMockWithSecret(`{invalid json`)
	setupMockFactory(mock)

	err := LoadSecretsIntoEnv(context.Background())
	require.Error(t, err, "invalid JSON must result in an error")
	assert.Contains(t, err.Error(), "failed to parse secret JSON")
}

// TestLoadSecretsIntoEnv_EmptyJSON_Noop verifies that an empty JSON object
// in the secret causes no error and injects nothing.
func TestLoadSecretsIntoEnv_EmptyJSON_Noop(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")

	mock := newMockWithSecret(`{}`)
	setupMockFactory(mock)

	err := LoadSecretsIntoEnv(context.Background())
	require.NoError(t, err)
}

// TestLoadSecretsIntoEnv_NilSecretString_Noop verifies that when the SM
// response has a nil SecretString, the function returns nil without error.
func TestLoadSecretsIntoEnv_NilSecretString_Noop(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")

	mock := &mockSMClient{secretString: nil}
	setupMockFactory(mock)

	err := LoadSecretsIntoEnv(context.Background())
	require.NoError(t, err, "nil SecretString must not cause an error")
	assert.Equal(t, 1, mock.callCount, "SM should still be called once")
}

// TestLoadSecretsIntoEnv_NestedJSON_SkipsNonString verifies that non-string
// values (objects, arrays, numbers, booleans, null) in the JSON are silently
// skipped and only string values are injected as env vars.
func TestLoadSecretsIntoEnv_NestedJSON_SkipsNonString(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	unsetEnv(t, "SM_STRING_VAR")
	unsetEnv(t, "SM_NESTED_VAR")
	unsetEnv(t, "SM_ARRAY_VAR")
	unsetEnv(t, "SM_NUM_VAR")
	unsetEnv(t, "SM_BOOL_VAR")
	unsetEnv(t, "SM_NULL_VAR")

	mock := newMockWithSecret(`{
		"SM_STRING_VAR": "hello",
		"SM_NESTED_VAR": {"key": "value"},
		"SM_ARRAY_VAR": [1, 2, 3],
		"SM_NUM_VAR": 42,
		"SM_BOOL_VAR": true,
		"SM_NULL_VAR": null
	}`)
	setupMockFactory(mock)

	err := LoadSecretsIntoEnv(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "hello", os.Getenv("SM_STRING_VAR"), "string value should be injected")
	assert.Empty(t, os.Getenv("SM_NESTED_VAR"), "nested object must not be injected")
	assert.Empty(t, os.Getenv("SM_ARRAY_VAR"), "array must not be injected")
	assert.Empty(t, os.Getenv("SM_NUM_VAR"), "number must not be injected")
	assert.Empty(t, os.Getenv("SM_BOOL_VAR"), "boolean must not be injected")
	assert.Empty(t, os.Getenv("SM_NULL_VAR"), "null must not be injected")
}

// --- Region handling tests ---

// TestLoadSecretsIntoEnv_DefaultRegion_UsEast1 verifies that when
// SECRET_MANAGER_REGION is unset, the default region "us-east-1" is used.
func TestLoadSecretsIntoEnv_DefaultRegion_UsEast1(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	unsetEnv(t, envSecretManagerRegion)

	mock := newMockWithSecret(`{}`)
	var capturedRegion string
	setupMockFactoryWithRegionCapture(mock, &capturedRegion)

	err := LoadSecretsIntoEnv(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "us-east-1", capturedRegion,
		"when SECRET_MANAGER_REGION is unset, default region must be us-east-1")
}

// TestLoadSecretsIntoEnv_CustomRegion verifies that SECRET_MANAGER_REGION
// is correctly passed to the client factory.
func TestLoadSecretsIntoEnv_CustomRegion(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	t.Setenv(envSecretManagerRegion, "ap-northeast-1")

	mock := newMockWithSecret(`{}`)
	var capturedRegion string
	setupMockFactoryWithRegionCapture(mock, &capturedRegion)

	err := LoadSecretsIntoEnv(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "ap-northeast-1", capturedRegion,
		"custom SECRET_MANAGER_REGION must be forwarded to the client factory")
}

// --- Client factory error test ---

// TestLoadSecretsIntoEnv_ClientFactoryError verifies that when smClientFactory
// returns an error, it is propagated correctly.
func TestLoadSecretsIntoEnv_ClientFactoryError(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")

	smClientFactory = func(ctx context.Context, region string) (SecretsManagerClient, error) {
		return nil, errors.New("unable to load AWS credentials")
	}

	err := LoadSecretsIntoEnv(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create Secrets Manager client")
	assert.Contains(t, err.Error(), "unable to load AWS credentials")
}

// --- sync.Once behavior tests ---

// TestLoadSecretsIntoEnv_SyncOnce_CalledOnce verifies that even when Load()
// and LoadSlack() are both called, Secrets Manager is contacted exactly once.
func TestLoadSecretsIntoEnv_SyncOnce_CalledOnce(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	setMinRequiredLoadEnvVars(t)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	t.Setenv("SLACK_BOT_TOKEN", "pre-existing-token") // satisfy required=true

	mock := newMockWithSecret(`{}`)
	setupMockFactory(mock)

	// First call via Load() — triggers sync.Once, SM GetSecretValue called once.
	_, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 1, mock.callCount, "SM should be called once after Load()")

	// Second call via LoadSlack() — sync.Once already fired, SM NOT called again.
	_, err = LoadSlack()
	require.NoError(t, err)
	assert.Equal(t, 1, mock.callCount,
		"SM must be called exactly once despite both Load() and LoadSlack() being invoked")
}

// TestLoadSecretsIntoEnv_SyncOnce_CachesError verifies that if the first
// invocation fails, subsequent calls return the same cached error without
// re-contacting Secrets Manager.
func TestLoadSecretsIntoEnv_SyncOnce_CachesError(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")

	mock := &mockSMClient{returnErr: errors.New("transient failure")}
	setupMockFactory(mock)

	err1 := LoadSecretsIntoEnv(context.Background())
	require.Error(t, err1)
	assert.Equal(t, 1, mock.callCount)

	// Second call must return the cached error without calling SM again.
	err2 := LoadSecretsIntoEnv(context.Background())
	require.Error(t, err2)
	assert.Equal(t, err1, err2, "cached error must be returned on subsequent calls")
	assert.Equal(t, 1, mock.callCount, "SM must not be called again after cached failure")
}

// --- End-to-end integration tests with Load() / LoadSlack() ---

// setMinRequiredLoadEnvVars sets the minimum env vars needed for Load() to
// succeed without real AWS/OpenSearch infrastructure.
func setMinRequiredLoadEnvVars(t *testing.T) {
	t.Helper()
	t.Setenv("OPENSEARCH_ENDPOINT", "http://localhost:9200")
	t.Setenv("OPENSEARCH_INDEX", "test_index")
	t.Setenv("VECTOR_DB_BACKEND", "sqlite")
	t.Setenv("AWS_S3_VECTOR_BUCKET", "")
	t.Setenv("AWS_S3_VECTOR_INDEX", "")
}

// TestLoadWithSecretsManager_EndToEnd verifies that when Load() is called with
// SECRET_MANAGER_SECRET_ID set, SM-injected values appear in the returned Config.
// The target env var (GITHUB_TOKEN) must be truly unset for the fallback to work.
func TestLoadWithSecretsManager_EndToEnd(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	setMinRequiredLoadEnvVars(t)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	unsetEnv(t, "GITHUB_TOKEN") // truly remove so SM fallback injects it

	mock := newMockWithSecret(`{"GITHUB_TOKEN":"sm-injected-github-token"}`)
	setupMockFactory(mock)

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "sm-injected-github-token", cfg.GitHubToken,
		"GITHUB_TOKEN should be injected from Secrets Manager into Config")
}

// TestLoadWithSecretsManager_EnvTakesPriority verifies that when GITHUB_TOKEN
// is already set, Load() does NOT overwrite it with the SM value.
func TestLoadWithSecretsManager_EnvTakesPriority(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	setMinRequiredLoadEnvVars(t)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	t.Setenv("GITHUB_TOKEN", "env-value")

	mock := newMockWithSecret(`{"GITHUB_TOKEN":"sm-should-not-win"}`)
	setupMockFactory(mock)

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "env-value", cfg.GitHubToken,
		"existing GITHUB_TOKEN must take priority over Secrets Manager value")
}

// TestLoadSlackWithSecretsManager_EndToEnd verifies that when LoadSlack() is
// called with SECRET_MANAGER_SECRET_ID set, SM-injected values appear in SlackConfig.
// The SLACK_BOT_TOKEN must be truly unset for the fallback to work.
func TestLoadSlackWithSecretsManager_EndToEnd(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	unsetEnv(t, "SLACK_BOT_TOKEN") // truly remove so SM fallback injects it

	mock := newMockWithSecret(`{"SLACK_BOT_TOKEN":"sm-slack-bot-token"}`)
	setupMockFactory(mock)

	cfg, err := LoadSlack()
	require.NoError(t, err)
	assert.Equal(t, "sm-slack-bot-token", cfg.BotToken,
		"SLACK_BOT_TOKEN should be injected from Secrets Manager into SlackConfig")
}

// TestLoadWithSecretsManager_MultipleSecrets verifies that SM can inject
// multiple config fields at once through a single secret JSON.
func TestLoadWithSecretsManager_MultipleSecrets(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	setMinRequiredLoadEnvVars(t)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	unsetEnv(t, "GITHUB_TOKEN")
	unsetEnv(t, "SLACK_USER_TOKEN")
	unsetEnv(t, "GEMINI_API_KEY")

	mock := newMockWithSecret(`{
		"GITHUB_TOKEN": "sm-github",
		"SLACK_USER_TOKEN": "sm-slack-user",
		"GEMINI_API_KEY": "sm-gemini"
	}`)
	setupMockFactory(mock)

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "sm-github", cfg.GitHubToken)
	assert.Equal(t, "sm-slack-user", cfg.SlackUserToken)
	assert.Equal(t, "sm-gemini", cfg.GeminiAPIKey)
}

// TestLoadWithSecretsManager_NoSecretID_NoSMCall verifies that when
// SECRET_MANAGER_SECRET_ID is not configured, Load() still works normally
// and no SM client is ever created.
func TestLoadWithSecretsManager_NoSecretID_NoSMCall(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	setMinRequiredLoadEnvVars(t)
	unsetEnv(t, envSecretManagerSecretID)
	t.Setenv("GITHUB_TOKEN", "direct-env-token")

	called := false
	smClientFactory = func(ctx context.Context, region string) (SecretsManagerClient, error) {
		called = true
		return nil, errors.New("should not be called")
	}

	cfg, err := Load()
	require.NoError(t, err)
	assert.False(t, called, "SM client factory must not be called without SECRET_MANAGER_SECRET_ID")
	assert.Equal(t, "direct-env-token", cfg.GitHubToken)
}
