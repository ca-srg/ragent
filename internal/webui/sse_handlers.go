package webui

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// handleSSEProgress handles the SSE progress endpoint
func (s *Server) handleSSEProgress(w http.ResponseWriter, r *http.Request) {
	s.handleSSE(w, r, []string{
		EventTypeVectorizeStarted,
		EventTypeVectorizeProgress,
		EventTypeVectorizeCompleted,
		EventTypeVectorizeFailed,
		EventTypeHeartbeat,
	})
}

// handleSSEEvents handles the general SSE events endpoint
func (s *Server) handleSSEEvents(w http.ResponseWriter, r *http.Request) {
	// Parse filter query parameter
	filterStr := r.URL.Query().Get("filter")
	var filters []string
	if filterStr != "" {
		filters = strings.Split(filterStr, ",")
	}

	s.handleSSE(w, r, filters)
}

// handleSSE is the common SSE handler
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request, filters []string) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Create flusher
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Register client
	clientID := uuid.New().String()
	client, err := s.sseManager.RegisterClient(clientID, filters)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer s.sseManager.UnregisterClient(clientID)

	// Send initial connection event
	_, _ = fmt.Fprintf(w, "event: connected\ndata: {\"client_id\":\"%s\"}\n\n", clientID)
	flusher.Flush()

	// Stream events
	for {
		select {
		case <-r.Context().Done():
			return
		case <-client.Done:
			return
		case data, ok := <-client.Events:
			if !ok {
				return
			}
			_, _ = w.Write(data)
			flusher.Flush()
		}
	}
}
