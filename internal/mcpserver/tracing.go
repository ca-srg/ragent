package mcpserver

import "go.opentelemetry.io/otel"

var (
	mcpTracer = otel.Tracer("ragent/mcpserver")
)
