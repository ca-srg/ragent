package webui

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSSEManager(t *testing.T) {
	manager := NewSSEManager(nil, nil)

	assert.NotNil(t, manager)
	assert.NotNil(t, manager.config)
	assert.Equal(t, 30*time.Second, manager.config.HeartbeatInterval)
	assert.Equal(t, 100, manager.config.BufferSize)
	assert.Equal(t, 100, manager.config.MaxClients)
	assert.Equal(t, 0, manager.GetClientCount())
}

func TestNewSSEManagerWithConfig(t *testing.T) {
	config := &SSEConfig{
		HeartbeatInterval: 10 * time.Second,
		BufferSize:        50,
		MaxClients:        25,
	}
	logger := log.New(io.Discard, "", 0)

	manager := NewSSEManager(config, logger)

	assert.Equal(t, 10*time.Second, manager.config.HeartbeatInterval)
	assert.Equal(t, 50, manager.config.BufferSize)
	assert.Equal(t, 25, manager.config.MaxClients)
}

func TestSSEManagerStartStop(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	manager := NewSSEManager(&SSEConfig{
		HeartbeatInterval: 1 * time.Hour,
		BufferSize:        10,
		MaxClients:        10,
	}, logger)

	manager.Start(context.Background())
	assert.NotNil(t, manager.ctx)
	assert.NotNil(t, manager.cancel)

	manager.Stop()
	// Stop should be idempotent
	manager.Stop()
}

func TestSSEManagerRegisterClient(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	manager := NewSSEManager(&SSEConfig{
		HeartbeatInterval: 1 * time.Hour,
		BufferSize:        10,
		MaxClients:        10,
	}, logger)
	manager.Start(context.Background())
	defer manager.Stop()

	client, err := manager.RegisterClient("client1", nil)
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, "client1", client.ID)
	assert.NotNil(t, client.Events)
	assert.NotNil(t, client.Done)
	assert.Equal(t, 1, manager.GetClientCount())
}

func TestSSEManagerRegisterClientWithFilters(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	manager := NewSSEManager(&SSEConfig{
		HeartbeatInterval: 1 * time.Hour,
		BufferSize:        10,
		MaxClients:        10,
	}, logger)
	manager.Start(context.Background())
	defer manager.Stop()

	filters := []string{EventTypeVectorizeProgress, EventTypeVectorizeCompleted}
	client, err := manager.RegisterClient("client1", filters)

	require.NoError(t, err)
	assert.Equal(t, filters, client.Filters)
}

func TestSSEManagerMaxClients(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	manager := NewSSEManager(&SSEConfig{
		HeartbeatInterval: 1 * time.Hour,
		BufferSize:        10,
		MaxClients:        2,
	}, logger)
	manager.Start(context.Background())
	defer manager.Stop()

	_, err := manager.RegisterClient("client1", nil)
	require.NoError(t, err)

	_, err = manager.RegisterClient("client2", nil)
	require.NoError(t, err)

	_, err = manager.RegisterClient("client3", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "maximum")
}

func TestSSEManagerUnregisterClient(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	manager := NewSSEManager(&SSEConfig{
		HeartbeatInterval: 1 * time.Hour,
		BufferSize:        10,
		MaxClients:        10,
	}, logger)
	manager.Start(context.Background())
	defer manager.Stop()

	_, err := manager.RegisterClient("client1", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, manager.GetClientCount())

	manager.UnregisterClient("client1")
	assert.Equal(t, 0, manager.GetClientCount())

	// Should be idempotent
	manager.UnregisterClient("client1")
	assert.Equal(t, 0, manager.GetClientCount())
}

func TestSSEManagerSendEvent(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	manager := NewSSEManager(&SSEConfig{
		HeartbeatInterval: 1 * time.Hour,
		BufferSize:        10,
		MaxClients:        10,
	}, logger)
	manager.Start(context.Background())
	defer manager.Stop()

	client, err := manager.RegisterClient("client1", nil)
	require.NoError(t, err)

	event := &SSEEvent{
		Event: EventTypeVectorizeProgress,
		Data: map[string]interface{}{
			"status": "running",
		},
	}

	manager.SendEvent(event)

	// Wait for event to be dispatched
	select {
	case msg := <-client.Events:
		assert.Contains(t, string(msg), "event: vectorize_progress")
		assert.Contains(t, string(msg), "running")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("event not received")
	}
}

func TestSSEManagerSendEventWithFilters(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	manager := NewSSEManager(&SSEConfig{
		HeartbeatInterval: 1 * time.Hour,
		BufferSize:        10,
		MaxClients:        10,
	}, logger)
	manager.Start(context.Background())
	defer manager.Stop()

	// Client 1 only wants progress events
	client1, _ := manager.RegisterClient("client1", []string{EventTypeVectorizeProgress})
	// Client 2 wants all events
	client2, _ := manager.RegisterClient("client2", nil)

	// Send a completed event (should only go to client2)
	manager.SendEvent(&SSEEvent{
		Event: EventTypeVectorizeCompleted,
		Data:  map[string]interface{}{},
	})

	// Wait for event processing
	time.Sleep(50 * time.Millisecond)

	select {
	case <-client1.Events:
		t.Fatal("client1 should not receive completed event")
	default:
		// Expected
	}

	select {
	case msg := <-client2.Events:
		assert.Contains(t, string(msg), "vectorize_completed")
	default:
		t.Fatal("client2 should receive completed event")
	}
}

func TestSSEManagerBroadcastToMultipleClients(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	manager := NewSSEManager(&SSEConfig{
		HeartbeatInterval: 1 * time.Hour,
		BufferSize:        10,
		MaxClients:        10,
	}, logger)
	manager.Start(context.Background())
	defer manager.Stop()

	client1, _ := manager.RegisterClient("client1", nil)
	client2, _ := manager.RegisterClient("client2", nil)

	manager.SendEvent(&SSEEvent{
		Event: EventTypeVectorizeProgress,
		Data:  map[string]interface{}{"test": "data"},
	})

	// Wait for event processing
	time.Sleep(50 * time.Millisecond)

	select {
	case msg := <-client1.Events:
		assert.Contains(t, string(msg), "vectorize_progress")
	default:
		t.Fatal("client1 should receive event")
	}

	select {
	case msg := <-client2.Events:
		assert.Contains(t, string(msg), "vectorize_progress")
	default:
		t.Fatal("client2 should receive event")
	}
}

func TestFormatSSEMessage(t *testing.T) {
	data := []byte(`{"status":"running"}`)
	message := formatSSEMessage("test_event", data)

	assert.Contains(t, string(message), "event: test_event")
	assert.Contains(t, string(message), `data: {"status":"running"}`)
	assert.Contains(t, string(message), "\n\n")
}

func TestSSEManagerShouldSendToClient(t *testing.T) {
	manager := NewSSEManager(nil, nil)

	tests := []struct {
		name      string
		filters   []string
		eventType string
		expected  bool
	}{
		{
			name:      "no filters receives all",
			filters:   nil,
			eventType: EventTypeVectorizeProgress,
			expected:  true,
		},
		{
			name:      "empty filters receives all",
			filters:   []string{},
			eventType: EventTypeVectorizeProgress,
			expected:  true,
		},
		{
			name:      "matching filter",
			filters:   []string{EventTypeVectorizeProgress},
			eventType: EventTypeVectorizeProgress,
			expected:  true,
		},
		{
			name:      "non-matching filter",
			filters:   []string{EventTypeVectorizeProgress},
			eventType: EventTypeVectorizeCompleted,
			expected:  false,
		},
		{
			name:      "multiple filters match",
			filters:   []string{EventTypeVectorizeProgress, EventTypeVectorizeCompleted},
			eventType: EventTypeVectorizeCompleted,
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &SSEClient{Filters: tt.filters}
			result := manager.shouldSendToClient(client, tt.eventType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSSEManagerClientClosesSafely(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	manager := NewSSEManager(&SSEConfig{
		HeartbeatInterval: 1 * time.Hour,
		BufferSize:        10,
		MaxClients:        10,
	}, logger)
	manager.Start(context.Background())
	defer manager.Stop()

	client, _ := manager.RegisterClient("client1", nil)

	// Unregister closes channels
	manager.UnregisterClient("client1")

	// Verify Done channel is closed
	select {
	case <-client.Done:
		// Expected
	default:
		t.Fatal("Done channel should be closed")
	}
}

func TestSSEManagerConcurrentAccess(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	manager := NewSSEManager(&SSEConfig{
		HeartbeatInterval: 1 * time.Hour,
		BufferSize:        100,
		MaxClients:        100,
	}, logger)
	manager.Start(context.Background())
	defer manager.Stop()

	done := make(chan bool)

	// Concurrent register/unregister
	go func() {
		for i := 0; i < 20; i++ {
			client, _ := manager.RegisterClient("goroutine1", nil)
			if client != nil {
				manager.UnregisterClient("goroutine1")
			}
		}
		done <- true
	}()

	// Concurrent send events
	go func() {
		for i := 0; i < 50; i++ {
			manager.SendEvent(&SSEEvent{
				Event: EventTypeVectorizeProgress,
				Data:  map[string]interface{}{"i": i},
			})
		}
		done <- true
	}()

	// Concurrent get client count
	go func() {
		for i := 0; i < 50; i++ {
			_ = manager.GetClientCount()
		}
		done <- true
	}()

	<-done
	<-done
	<-done
}
