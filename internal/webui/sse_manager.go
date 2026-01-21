package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// SSEConfig holds configuration for the SSE manager
type SSEConfig struct {
	HeartbeatInterval time.Duration
	BufferSize        int
	MaxClients        int
}

// SSEClient represents a connected SSE client
type SSEClient struct {
	ID       string
	Events   chan []byte
	Filters  []string // Event types to receive (empty = all)
	Done     chan struct{}
	mu       sync.Mutex
	isClosed bool
}

// SSEManager manages Server-Sent Events connections
type SSEManager struct {
	clients    map[string]*SSEClient
	mu         sync.RWMutex
	config     *SSEConfig
	logger     *log.Logger
	ctx        context.Context
	cancel     context.CancelFunc
	eventQueue chan *SSEEvent
}

// NewSSEManager creates a new SSE manager
func NewSSEManager(config *SSEConfig, logger *log.Logger) *SSEManager {
	if config == nil {
		config = &SSEConfig{
			HeartbeatInterval: 30 * time.Second,
			BufferSize:        100,
			MaxClients:        100,
		}
	}
	if logger == nil {
		logger = log.Default()
	}

	return &SSEManager{
		clients:    make(map[string]*SSEClient),
		config:     config,
		logger:     logger,
		eventQueue: make(chan *SSEEvent, config.BufferSize),
	}
}

// Start starts the SSE manager
func (m *SSEManager) Start(ctx context.Context) {
	m.ctx, m.cancel = context.WithCancel(ctx)
	go m.heartbeatLoop()
	go m.eventDispatcher()
}

// Stop stops the SSE manager
func (m *SSEManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, client := range m.clients {
		m.closeClient(client)
	}
	m.clients = make(map[string]*SSEClient)
}

// RegisterClient registers a new SSE client
func (m *SSEManager) RegisterClient(id string, filters []string) (*SSEClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.clients) >= m.config.MaxClients {
		return nil, fmt.Errorf("maximum number of SSE clients reached")
	}

	client := &SSEClient{
		ID:      id,
		Events:  make(chan []byte, m.config.BufferSize),
		Filters: filters,
		Done:    make(chan struct{}),
	}

	m.clients[id] = client
	m.logger.Printf("SSE client registered: %s (total: %d)", id, len(m.clients))
	return client, nil
}

// UnregisterClient removes an SSE client
func (m *SSEManager) UnregisterClient(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if client, ok := m.clients[id]; ok {
		m.closeClient(client)
		delete(m.clients, id)
		m.logger.Printf("SSE client unregistered: %s (remaining: %d)", id, len(m.clients))
	}
}

// closeClient closes a client's channels safely
func (m *SSEManager) closeClient(client *SSEClient) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if !client.isClosed {
		client.isClosed = true
		close(client.Done)
		close(client.Events)
	}
}

// SendEvent sends an event to all connected clients
func (m *SSEManager) SendEvent(event *SSEEvent) {
	select {
	case m.eventQueue <- event:
	default:
		m.logger.Printf("SSE event queue full, dropping event: %s", event.Event)
	}
}

// eventDispatcher dispatches events to clients
func (m *SSEManager) eventDispatcher() {
	for {
		select {
		case <-m.ctx.Done():
			return
		case event := <-m.eventQueue:
			m.broadcastEvent(event)
		}
	}
}

// broadcastEvent broadcasts an event to all matching clients
func (m *SSEManager) broadcastEvent(event *SSEEvent) {
	data, err := json.Marshal(event.Data)
	if err != nil {
		m.logger.Printf("Failed to marshal SSE event data: %v", err)
		return
	}

	message := formatSSEMessage(event.Event, data)

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, client := range m.clients {
		if m.shouldSendToClient(client, event.Event) {
			select {
			case client.Events <- message:
			default:
				m.logger.Printf("SSE client %s buffer full, dropping event", client.ID)
			}
		}
	}
}

// shouldSendToClient checks if the event should be sent to the client
func (m *SSEManager) shouldSendToClient(client *SSEClient, eventType string) bool {
	if len(client.Filters) == 0 {
		return true
	}
	for _, filter := range client.Filters {
		if filter == eventType {
			return true
		}
	}
	return false
}

// heartbeatLoop sends heartbeat events to keep connections alive
func (m *SSEManager) heartbeatLoop() {
	ticker := time.NewTicker(m.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.SendEvent(&SSEEvent{
				Event: EventTypeHeartbeat,
				Data: map[string]interface{}{
					"timestamp": time.Now().Format(time.RFC3339),
				},
			})
		}
	}
}

// formatSSEMessage formats an SSE message
func formatSSEMessage(event string, data []byte) []byte {
	return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(data)))
}

// GetClientCount returns the number of connected clients
func (m *SSEManager) GetClientCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}
