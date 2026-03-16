package config

// SecretAccessChecker evaluates whether a user can access documents marked as secret.
type SecretAccessChecker struct {
	slackConfig *SlackSecretConfig
}

// NewSecretAccessChecker creates a new checker instance.
func NewSecretAccessChecker(slackConfig *SlackSecretConfig) *SecretAccessChecker {
	return &SecretAccessChecker{
		slackConfig: slackConfig,
	}
}

// CanAccessSecret returns true if the caller may access documents with secret=true.
//
// Rules:
// - OIDC-authenticated users can always access secrets.
// - Users listed in the Slack allowlist can access secrets.
// - All others cannot.
func (c *SecretAccessChecker) CanAccessSecret(isOIDCAuthenticated bool, slackUserID string) bool {
	if isOIDCAuthenticated {
		return true
	}
	if c == nil || c.slackConfig == nil {
		return false
	}
	if c.slackConfig.IsAllowed(slackUserID) {
		return true
	}
	return false
}
