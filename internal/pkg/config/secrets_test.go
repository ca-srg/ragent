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
	secretString string
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
		SecretString: aws.String(m.secretString),
	}, nil
}

// setupMockFactory replaces smClientFactory with a function returning the given mock.
func setupMockFactory(mock *mockSMClient) {
	smClientFactory = func(ctx context.Context, region string) (SecretsManagerClient, error) {
		return mock, nil
	}
}

// TestLoadSecretsIntoEnv_SecretIDNotSet_Noop verifies that when
// SECRET_MANAGER_SECRET_ID is not set, LoadSecretsIntoEnv does nothing.
func TestLoadSecretsIntoEnv_SecretIDNotSet_Noop(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	// Ensure the env var is empty (unset state)
	t.Setenv(envSecretManagerSecretID, "")
	called := false
	smClientFactory = func(ctx context.Context, region string) (SecretsManagerClient, error) {
		called = true
		return nil, nil
	}

	err := LoadSecretsIntoEnv(context.Background())
	require.NoError(t, err)
	assert.False(t, called, "smClientFactory must not be called when SECRET_MANAGER_SECRET_ID is not set")
}

// TestLoadSecretsIntoEnv_Success_InjectsEnvVars verifies that when SM returns
// a valid JSON secret, unset env vars are populated from it.
func TestLoadSecretsIntoEnv_Success_InjectsEnvVars(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	t.Setenv(envSecretManagerRegion, "us-west-2")
	// Ensure target vars start empty so injection is exercised
	t.Setenv("TEST_SM_INJECT_A", "")
	t.Setenv("TEST_SM_INJECT_B", "")

	mock := &mockSMClient{
		secretString: `{"TEST_SM_INJECT_A":"value-a","TEST_SM_INJECT_B":"value-b"}`,
	}
	setupMockFactory(mock)

	err := LoadSecretsIntoEnv(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "value-a", os.Getenv("TEST_SM_INJECT_A"), "TEST_SM_INJECT_A should be injected from SM")
	assert.Equal(t, "value-b", os.Getenv("TEST_SM_INJECT_B"), "TEST_SM_INJECT_B should be injected from SM")
}

// TestLoadSecretsIntoEnv_ExistingEnvNotOverwritten verifies that env vars
// already set in the environment are NOT overwritten by SM values.
func TestLoadSecretsIntoEnv_ExistingEnvNotOverwritten(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	t.Setenv("EXISTING_VAR_SM", "original-value")

	mock := &mockSMClient{
		secretString: `{"EXISTING_VAR_SM":"sm-overwrite-attempt","NEW_VAR_SM":"new-value"}`,
	}
	setupMockFactory(mock)

	err := LoadSecretsIntoEnv(context.Background())
	require.NoError(t, err)
	// Existing env var must NOT be overwritten by SM value
	assert.Equal(t, "original-value", os.Getenv("EXISTING_VAR_SM"),
		"existing env var must not be overwritten by Secrets Manager")
}

// TestLoadSecretsIntoEnv_SMError_ReturnsFatal verifies that when the SM client
// returns an error, LoadSecretsIntoEnv propagates it as a non-nil error.
func TestLoadSecretsIntoEnv_SMError_ReturnsFatal(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "bad-secret-id")

	mock := &mockSMClient{
		returnErr: errors.New("access denied by IAM policy"),
	}
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

	mock := &mockSMClient{
		secretString: `{invalid json`,
	}
	setupMockFactory(mock)

	err := LoadSecretsIntoEnv(context.Background())
	require.Error(t, err, "invalid JSON must result in an error")
}

// TestLoadSecretsIntoEnv_EmptyJSON_Noop verifies that an empty JSON object
// in the secret causes no error and injects nothing.
func TestLoadSecretsIntoEnv_EmptyJSON_Noop(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")

	mock := &mockSMClient{
		secretString: `{}`,
	}
	setupMockFactory(mock)

	err := LoadSecretsIntoEnv(context.Background())
	require.NoError(t, err)
}

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
func TestLoadWithSecretsManager_EndToEnd(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	setMinRequiredLoadEnvVars(t)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	t.Setenv("GITHUB_TOKEN", "") // ensure it's empty so SM can inject it

	mock := &mockSMClient{
		secretString: `{"GITHUB_TOKEN":"sm-injected-github-token"}`,
	}
	setupMockFactory(mock)

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "sm-injected-github-token", cfg.GitHubToken,
		"GITHUB_TOKEN should be injected from Secrets Manager into Config")
}

// TestLoadSlackWithSecretsManager_EndToEnd verifies that when LoadSlack() is
// called with SECRET_MANAGER_SECRET_ID set, SM-injected values appear in SlackConfig.
func TestLoadSlackWithSecretsManager_EndToEnd(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	t.Setenv("SLACK_BOT_TOKEN", "") // required=true; SM will inject it

	mock := &mockSMClient{
		secretString: `{"SLACK_BOT_TOKEN":"sm-slack-bot-token"}`,
	}
	setupMockFactory(mock)

	cfg, err := LoadSlack()
	require.NoError(t, err)
	assert.Equal(t, "sm-slack-bot-token", cfg.BotToken,
		"SLACK_BOT_TOKEN should be injected from Secrets Manager into SlackConfig")
}

// TestLoadSecretsIntoEnv_SyncOnce_CalledOnce verifies that even when Load() and
// LoadSlack() are both called, Secrets Manager is contacted exactly once.
func TestLoadSecretsIntoEnv_SyncOnce_CalledOnce(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	setMinRequiredLoadEnvVars(t)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	t.Setenv("SLACK_BOT_TOKEN", "pre-existing-token") // satisfy required=true

	mock := &mockSMClient{secretString: `{}`}
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

// TestLoadSecretsIntoEnv_NestedJSON_SkipsNonString verifies that non-string
// values (objects, arrays, numbers) in the JSON are silently skipped and only
// string values are injected as env vars.
func TestLoadSecretsIntoEnv_NestedJSON_SkipsNonString(t *testing.T) {
	t.Cleanup(ResetSecretsLoaderForTest)
	t.Setenv(envSecretManagerSecretID, "my-secret-id")
	// Ensure target vars start empty
	t.Setenv("SM_STRING_VAR", "")
	t.Setenv("SM_NESTED_VAR", "")
	t.Setenv("SM_ARRAY_VAR", "")
	t.Setenv("SM_NUM_VAR", "")

	mock := &mockSMClient{
		secretString: `{
			"SM_STRING_VAR": "hello",
			"SM_NESTED_VAR": {"key": "value"},
			"SM_ARRAY_VAR": [1, 2, 3],
			"SM_NUM_VAR": 42
		}`,
	}
	setupMockFactory(mock)

	err := LoadSecretsIntoEnv(context.Background())
	require.NoError(t, err)
	// Only string values should be injected
	assert.Equal(t, "hello", os.Getenv("SM_STRING_VAR"), "string value should be injected")
	// Non-string values should be skipped (remain empty)
	assert.Empty(t, os.Getenv("SM_NESTED_VAR"), "nested object must not be injected")
	assert.Empty(t, os.Getenv("SM_ARRAY_VAR"), "array must not be injected")
	assert.Empty(t, os.Getenv("SM_NUM_VAR"), "number must not be injected")
}
