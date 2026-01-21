package webui

import (
	"context"
	"log"
	"sync"
	"time"
)

const (
	defaultSchedulerInterval = 30 * time.Minute
	minSchedulerInterval     = 5 * time.Minute
)

// SchedulerRunFunc is the function called when the scheduler triggers
type SchedulerRunFunc func(ctx context.Context) error

// Scheduler manages the follow mode scheduling
type Scheduler struct {
	mu        sync.RWMutex
	enabled   bool
	interval  time.Duration
	nextRunAt time.Time
	lastRunAt time.Time
	state     *VectorizeState
	runFunc   SchedulerRunFunc
	logger    *log.Logger
	cancel    context.CancelFunc
	ticker    *time.Ticker
}

// NewScheduler creates a new scheduler
func NewScheduler(state *VectorizeState, interval time.Duration, logger *log.Logger) *Scheduler {
	if interval < minSchedulerInterval {
		interval = defaultSchedulerInterval
	}
	if logger == nil {
		logger = log.Default()
	}

	return &Scheduler{
		enabled:  false,
		interval: interval,
		state:    state,
		logger:   logger,
	}
}

// SetRunFunc sets the function to call when the scheduler triggers
func (s *Scheduler) SetRunFunc(fn SchedulerRunFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runFunc = fn
}

// Start starts the scheduler
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.enabled {
		return nil // Already running
	}

	if s.runFunc == nil {
		s.logger.Println("Scheduler: no run function set")
		return nil
	}

	s.enabled = true
	s.nextRunAt = time.Now().Add(s.interval)

	schedulerCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.ticker = time.NewTicker(s.interval)

	go s.loop(schedulerCtx)

	s.logger.Printf("Scheduler started with interval: %v, next run at: %s",
		s.interval, s.nextRunAt.Format("15:04:05"))

	s.sendSchedulerEvent()

	return nil
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled {
		return
	}

	s.enabled = false
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.ticker != nil {
		s.ticker.Stop()
		s.ticker = nil
	}
	s.nextRunAt = time.Time{}

	s.logger.Println("Scheduler stopped")
	s.sendSchedulerEvent()
}

// loop is the main scheduler loop
func (s *Scheduler) loop(ctx context.Context) {
	for {
		s.mu.RLock()
		ticker := s.ticker
		s.mu.RUnlock()

		if ticker == nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick handles a scheduler tick
func (s *Scheduler) tick(ctx context.Context) {
	s.mu.Lock()
	runFunc := s.runFunc
	if !s.enabled || runFunc == nil {
		s.mu.Unlock()
		return
	}

	// Check if already running
	if s.state.IsRunning() {
		s.logger.Println("Scheduler: skipping tick, vectorization already running")
		s.nextRunAt = time.Now().Add(s.interval)
		s.mu.Unlock()
		s.sendSchedulerEvent()
		return
	}

	s.lastRunAt = time.Now()
	s.nextRunAt = time.Now().Add(s.interval)
	s.mu.Unlock()

	s.sendSchedulerEvent()

	s.logger.Println("Scheduler: triggering vectorization run")
	if err := runFunc(ctx); err != nil {
		s.logger.Printf("Scheduler: run failed: %v", err)
	}
}

// SetInterval sets the scheduler interval
func (s *Scheduler) SetInterval(interval time.Duration) error {
	if interval < minSchedulerInterval {
		interval = minSchedulerInterval
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.interval = interval

	if s.enabled && s.ticker != nil {
		s.ticker.Reset(interval)
		s.nextRunAt = time.Now().Add(interval)
	}

	s.logger.Printf("Scheduler interval updated to: %v", interval)
	s.sendSchedulerEvent()

	return nil
}

// GetState returns the current scheduler state
func (s *Scheduler) GetState() *SchedulerState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &SchedulerState{
		Enabled:   s.enabled,
		Interval:  s.interval,
		NextRunAt: s.nextRunAt,
		LastRunAt: s.lastRunAt,
	}
}

// IsEnabled returns whether the scheduler is enabled
func (s *Scheduler) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

// sendSchedulerEvent sends a scheduler state update event via SSE
func (s *Scheduler) sendSchedulerEvent() {
	if s.state != nil && s.state.sseManager != nil {
		s.state.sseManager.SendEvent(&SSEEvent{
			Event: EventTypeSchedulerTick,
			Data:  s.GetState(),
		})
	}
}
