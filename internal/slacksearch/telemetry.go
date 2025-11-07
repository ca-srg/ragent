package slacksearch

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
)

var slackSearchTracer = otel.Tracer("ragent/slacksearch")

func telemetryFingerprint(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(trimmed))
	// Use first 8 bytes to keep fingerprints short while avoiding collisions.
	return fmt.Sprintf("%x", sum[:8])
}
