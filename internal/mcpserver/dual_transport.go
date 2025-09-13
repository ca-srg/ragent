package mcpserver

import (
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DualTransportHandler routes requests to either the Streamable HTTP handler or the SSE handler
// allowing both --transport http and --transport sse clients to work on the same path.
type DualTransportHandler struct {
	streamable *mcp.StreamableHTTPHandler
	sse        *mcp.SSEHandler
}

// NewDualTransportHandler creates a new DualTransportHandler.
func NewDualTransportHandler(getServer func(*http.Request) *mcp.Server) *DualTransportHandler {
	return &DualTransportHandler{
		streamable: mcp.NewStreamableHTTPHandler(getServer, nil),
		sse:        mcp.NewSSEHandler(getServer),
	}
}

// ServeHTTP dispatches requests to appropriate transport by inspecting method, headers, and query.
func (h *DualTransportHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// If SSE session POST (messages) -> query will contain sessionid
	if r.Method == http.MethodPost && r.URL.Query().Has("sessionid") {
		h.sse.ServeHTTP(w, r)
		return
	}

	// GET with Accept including text/event-stream -> start SSE session
	if r.Method == http.MethodGet {
		// Allow multiple Accept headers
		accept := strings.Split(strings.Join(r.Header.Values("Accept"), ","), ",")
		for _, c := range accept {
			if strings.TrimSpace(c) == "text/event-stream" || strings.HasPrefix(strings.TrimSpace(c), "text/") || strings.TrimSpace(c) == "*/*" {
				h.sse.ServeHTTP(w, r)
				return
			}
		}
		// Fallback to streamable (may 405 for GET without session)
		h.streamable.ServeHTTP(w, r)
		return
	}

	// DELETE and POST without sessionid -> streamable transport
	h.streamable.ServeHTTP(w, r)
}
