package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSecretAccessChecker_DefaultDeny(t *testing.T) {
	checker := NewSecretAccessChecker(&SlackSecretConfig{})
	assert.False(t, checker.CanAccessSecret(false, ""))
	assert.False(t, checker.CanAccessSecret(false, "U12345"))
}

func TestSecretAccessChecker_OIDCAllow(t *testing.T) {
	checker := NewSecretAccessChecker(&SlackSecretConfig{})
	assert.True(t, checker.CanAccessSecret(true, ""))
	assert.True(t, checker.CanAccessSecret(true, "U12345"))
}

func TestSecretAccessChecker_SlackAllowlistAllow(t *testing.T) {
	checker := NewSecretAccessChecker(&SlackSecretConfig{AllowedUsers: []string{"U12345"}})
	assert.True(t, checker.CanAccessSecret(false, "U12345"))
}

func TestSecretAccessChecker_SlackNotInAllowlist(t *testing.T) {
	checker := NewSecretAccessChecker(&SlackSecretConfig{AllowedUsers: []string{"U12345"}})
	assert.False(t, checker.CanAccessSecret(false, "U99999"))
}

func TestSecretAccessChecker_IPOnlyDeny(t *testing.T) {
	checker := NewSecretAccessChecker(&SlackSecretConfig{AllowedUsers: []string{""}})
	assert.False(t, checker.CanAccessSecret(false, ""))
}

func TestSecretAccessChecker_NilConfigDeny(t *testing.T) {
	var checker *SecretAccessChecker
	assert.True(t, checker.CanAccessSecret(true, "U12345"))
	assert.False(t, checker.CanAccessSecret(false, "U12345"))
}
