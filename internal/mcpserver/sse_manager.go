package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// SSEEvent represents a server-sent event
type SSEEvent struct {
	ID    string      `json:"id,omitempty"`
	Event string      `json:"event,omitempty"`
	Data  interface{} `json:"data"`
	Retry int         `json:"retry,omitempty"`
}

// SSEClient represents a connected SSE client
type SSEClient struct {
	ID           string
	Channel      chan *SSEEvent
	Context      context.Context
	Cancel       context.CancelFunc
	Request      *http.Request
	ConnectedAt  time.Time
	LastEventAt  time.Time
	EventFilters []string // Optional: filter events by type
}

// SSEManager manages SSE connections and event broadcasting
type SSEManager struct {
	clients           map[string]*SSEClient
	register          chan *SSEClient
	unregister        chan *SSEClient
	broadcast         chan *SSEEvent
	mutex             sync.RWMutex
	logger            *log.Logger
	heartbeatInterval time.Duration
	bufferSize        int
	maxClients        int
	eventHistory      []*SSEEvent // Optional: store recent events for replay
	historySize       int
}

// SSEManagerConfig contains configuration for SSE manager
type SSEManagerConfig struct {
	HeartbeatInterval time.Duration
	BufferSize        int
	MaxClients        int
	HistorySize       int
}

// DefaultSSEManagerConfig returns default SSE manager configuration
func DefaultSSEManagerConfig() *SSEManagerConfig {
	return &SSEManagerConfig{
		HeartbeatInterval: 30 * time.Second,
		BufferSize:        100,
		MaxClients:        1000,
		HistorySize:       50,
	}
}

// NewSSEManager creates a new SSE manager
func NewSSEManager(config *SSEManagerConfig, logger *log.Logger) *SSEManager {
	if config == nil {
		config = DefaultSSEManagerConfig()
	}

	return &SSEManager{
		clients:           make(map[string]*SSEClient),
		register:          make(chan *SSEClient),
		unregister:        make(chan *SSEClient),
		broadcast:         make(chan *SSEEvent, config.BufferSize),
		logger:            logger,
		heartbeatInterval: config.HeartbeatInterval,
		bufferSize:        config.BufferSize,
		maxClients:        config.MaxClients,
		eventHistory:      make([]*SSEEvent, 0, config.HistorySize),
		historySize:       config.HistorySize,
	}
}

// Start starts the SSE manager
func (m *SSEManager) Start(ctx context.Context) {
	go m.run(ctx)
}

// run is the main event loop for the SSE manager
func (m *SSEManager) run(ctx context.Context) {
	heartbeatTicker := time.NewTicker(m.heartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.closeAllClients()
			return

		case client := <-m.register:
			m.registerClient(client)

		case client := <-m.unregister:
			m.unregisterClient(client)

		case event := <-m.broadcast:
			m.broadcastEvent(event)

		case <-heartbeatTicker.C:
			m.sendHeartbeat()
		}
	}
}

// registerClient registers a new SSE client
func (m *SSEManager) registerClient(client *SSEClient) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if len(m.clients) >= m.maxClients {
		m.logger.Printf("Maximum number of clients reached (%d), rejecting client %s", m.maxClients, client.ID)
		client.Cancel()
		return
	}

	m.clients[client.ID] = client
	m.logger.Printf("SSE client registered: %s (total: %d)", client.ID, len(m.clients))

	// Send connection confirmation
	welcomeEvent := &SSEEvent{
		Event: "connected",
		Data: map[string]interface{}{
			"client_id": client.ID,
			"timestamp": time.Now().UTC(),
			"message":   "Successfully connected to SSE stream",
		},
	}

	select {
	case client.Channel <- welcomeEvent:
	default:
		m.logger.Printf("Failed to send welcome event to client %s", client.ID)
	}

	// Send event history if available
	if len(m.eventHistory) > 0 {
		m.sendEventHistory(client)
	}
}

// unregisterClient removes a client from the manager
func (m *SSEManager) unregisterClient(client *SSEClient) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.clients[client.ID]; exists {
		close(client.Channel)
		delete(m.clients, client.ID)
		m.logger.Printf("SSE client unregistered: %s (remaining: %d)", client.ID, len(m.clients))
	}
}

// broadcastEvent sends an event to all connected clients
func (m *SSEManager) broadcastEvent(event *SSEEvent) {
	m.mutex.RLock()
	clients := make([]*SSEClient, 0, len(m.clients))
	for _, client := range m.clients {
		clients = append(clients, client)
	}
	m.mutex.RUnlock()

	// Add to history
	m.addToHistory(event)

	// Send to all clients
	for _, client := range clients {
		// Check if client has filters
		if len(client.EventFilters) > 0 && !m.shouldSendEvent(client, event) {
			continue
		}

		select {
		case client.Channel <- event:
			client.LastEventAt = time.Now()
		default:
			// Client channel is full, remove client
			m.logger.Printf("Client %s channel full, removing", client.ID)
			go func(c *SSEClient) {
				m.unregister <- c
				c.Cancel()
			}(client)
		}
	}
}

// SendEvent sends an event to specific client or broadcasts to all
func (m *SSEManager) SendEvent(event *SSEEvent, clientID ...string) {
	if len(clientID) > 0 && clientID[0] != "" {
		// Send to specific client
		m.mutex.RLock()
		client, exists := m.clients[clientID[0]]
		m.mutex.RUnlock()

		if exists {
			select {
			case client.Channel <- event:
				client.LastEventAt = time.Now()
			default:
				m.logger.Printf("Failed to send event to client %s", clientID[0])
			}
		}
	} else {
		// Broadcast to all
		select {
		case m.broadcast <- event:
		default:
			m.logger.Printf("Broadcast channel full, dropping event")
		}
	}
}

// sendHeartbeat sends a heartbeat to all clients
func (m *SSEManager) sendHeartbeat() {
	heartbeat := &SSEEvent{
		Event: "heartbeat",
		Data: map[string]interface{}{
			"timestamp": time.Now().UTC(),
		},
	}

	m.mutex.RLock()
	clients := make([]*SSEClient, 0, len(m.clients))
	for _, client := range m.clients {
		clients = append(clients, client)
	}
	m.mutex.RUnlock()

	for _, client := range clients {
		select {
		case client.Channel <- heartbeat:
		default:
			// Channel might be full, but that's ok for heartbeats
		}
	}
}

// closeAllClients closes all client connections
func (m *SSEManager) closeAllClients() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, client := range m.clients {
		close(client.Channel)
		client.Cancel()
	}
	m.clients = make(map[string]*SSEClient)
	m.logger.Printf("All SSE clients closed")
}

// GetClientCount returns the number of connected clients
func (m *SSEManager) GetClientCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.clients)
}

// GetClientInfo returns information about connected clients
func (m *SSEManager) GetClientInfo() []map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	info := make([]map[string]interface{}, 0, len(m.clients))
	for _, client := range m.clients {
		info = append(info, map[string]interface{}{
			"id":           client.ID,
			"connected_at": client.ConnectedAt,
			"last_event":   client.LastEventAt,
			"remote_addr":  client.Request.RemoteAddr,
			"user_agent":   client.Request.UserAgent(),
		})
	}
	return info
}

// shouldSendEvent checks if an event should be sent to a client based on filters
func (m *SSEManager) shouldSendEvent(client *SSEClient, event *SSEEvent) bool {
	if len(client.EventFilters) == 0 {
		return true
	}

	for _, filter := range client.EventFilters {
		if filter == event.Event {
			return true
		}
	}
	return false
}

// addToHistory adds an event to the history buffer
func (m *SSEManager) addToHistory(event *SSEEvent) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.historySize <= 0 {
		return
	}

	m.eventHistory = append(m.eventHistory, event)
	if len(m.eventHistory) > m.historySize {
		m.eventHistory = m.eventHistory[len(m.eventHistory)-m.historySize:]
	}
}

// sendEventHistory sends historical events to a newly connected client
func (m *SSEManager) sendEventHistory(client *SSEClient) {
	for _, event := range m.eventHistory {
		if len(client.EventFilters) > 0 && !m.shouldSendEvent(client, event) {
			continue
		}

		select {
		case client.Channel <- event:
		default:
			// Skip if channel is full
		}
	}
}

// HandleSSE handles SSE connections
func (m *SSEManager) HandleSSE(w http.ResponseWriter, r *http.Request) {
	// Check if SSE is supported
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable Nginx buffering

	// Create client
	clientID := fmt.Sprintf("client-%d", time.Now().UnixNano())
	ctx, cancel := context.WithCancel(r.Context())

	client := &SSEClient{
		ID:          clientID,
		Channel:     make(chan *SSEEvent, m.bufferSize),
		Context:     ctx,
		Cancel:      cancel,
		Request:     r,
		ConnectedAt: time.Now(),
		LastEventAt: time.Now(),
	}

	// Parse event filters from query params
	if filters := r.URL.Query()["filter"]; len(filters) > 0 {
		client.EventFilters = filters
	}

	// Register client
	m.register <- client

	// Remove client on disconnect
	defer func() {
		m.unregister <- client
		cancel()
	}()

	// Send events to client
	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-client.Channel:
			if !ok {
				return
			}

			// Format SSE message
			if event.ID != "" {
				fmt.Fprintf(w, "id: %s\n", event.ID)
			}
			if event.Event != "" {
				fmt.Fprintf(w, "event: %s\n", event.Event)
			}
			if event.Retry > 0 {
				fmt.Fprintf(w, "retry: %d\n", event.Retry)
			}

			// Marshal data to JSON
			data, err := json.Marshal(event.Data)
			if err != nil {
				m.logger.Printf("Failed to marshal event data: %v", err)
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)

			// Flush the data immediately
			flusher.Flush()
		}
	}
}

// NotifyToolExecution sends a notification when a tool is executed
func (m *SSEManager) NotifyToolExecution(toolName string, params interface{}, result interface{}, err error) {
	event := &SSEEvent{
		Event: "tool_execution",
		Data: map[string]interface{}{
			"tool":      toolName,
			"params":    params,
			"timestamp": time.Now().UTC(),
		},
	}

	if err != nil {
		event.Data.(map[string]interface{})["error"] = err.Error()
		event.Data.(map[string]interface{})["status"] = "error"
	} else {
		event.Data.(map[string]interface{})["result"] = result
		event.Data.(map[string]interface{})["status"] = "success"
	}

	m.SendEvent(event)
}

// NotifySearchProgress sends search progress notifications
func (m *SSEManager) NotifySearchProgress(phase string, details interface{}) {
	event := &SSEEvent{
		Event: "search_progress",
		Data: map[string]interface{}{
			"phase":     phase,
			"details":   details,
			"timestamp": time.Now().UTC(),
		},
	}

	m.SendEvent(event)
}
