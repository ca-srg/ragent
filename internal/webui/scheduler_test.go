package webui

import (
	"context"
	"io"
	"log"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewScheduler(t *testing.T) {
	state := NewVectorizeState(nil)
	scheduler := NewScheduler(state, 30*time.Minute, nil)

	assert.NotNil(t, scheduler)
	assert.False(t, scheduler.IsEnabled())
	assert.Equal(t, 30*time.Minute, scheduler.interval)
}

func TestNewSchedulerMinInterval(t *testing.T) {
	state := NewVectorizeState(nil)
	// Interval less than minimum should be set to default
	scheduler := NewScheduler(state, 1*time.Minute, nil)

	assert.Equal(t, defaultSchedulerInterval, scheduler.interval)
}

func TestNewSchedulerWithLogger(t *testing.T) {
	state := NewVectorizeState(nil)
	logger := log.New(io.Discard, "", 0)
	scheduler := NewScheduler(state, 30*time.Minute, logger)

	assert.Equal(t, logger, scheduler.logger)
}

func TestSchedulerSetRunFunc(t *testing.T) {
	state := NewVectorizeState(nil)
	scheduler := NewScheduler(state, 30*time.Minute, nil)

	scheduler.SetRunFunc(func(ctx context.Context) error {
		return nil
	})

	assert.NotNil(t, scheduler.runFunc)
}

func TestSchedulerStartStop(t *testing.T) {
	state := NewVectorizeState(nil)
	logger := log.New(io.Discard, "", 0)
	scheduler := NewScheduler(state, 5*time.Minute, logger)

	scheduler.SetRunFunc(func(ctx context.Context) error {
		return nil
	})

	err := scheduler.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, scheduler.IsEnabled())

	schedulerState := scheduler.GetState()
	assert.True(t, schedulerState.Enabled)
	assert.NotZero(t, schedulerState.NextRunAt)

	scheduler.Stop()
	assert.False(t, scheduler.IsEnabled())

	schedulerState = scheduler.GetState()
	assert.False(t, schedulerState.Enabled)
	assert.Zero(t, schedulerState.NextRunAt)
}

func TestSchedulerStartWithoutRunFunc(t *testing.T) {
	state := NewVectorizeState(nil)
	logger := log.New(io.Discard, "", 0)
	scheduler := NewScheduler(state, 5*time.Minute, logger)

	// Start without setting run func
	err := scheduler.Start(context.Background())
	require.NoError(t, err)
	// Should not be enabled because no run func
	assert.False(t, scheduler.IsEnabled())
}

func TestSchedulerStartIdempotent(t *testing.T) {
	state := NewVectorizeState(nil)
	logger := log.New(io.Discard, "", 0)
	scheduler := NewScheduler(state, 5*time.Minute, logger)

	scheduler.SetRunFunc(func(ctx context.Context) error {
		return nil
	})

	err := scheduler.Start(context.Background())
	require.NoError(t, err)

	// Start again should be idempotent
	err = scheduler.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, scheduler.IsEnabled())

	scheduler.Stop()
}

func TestSchedulerStopIdempotent(t *testing.T) {
	state := NewVectorizeState(nil)
	logger := log.New(io.Discard, "", 0)
	scheduler := NewScheduler(state, 5*time.Minute, logger)

	// Stop when not running should be safe
	scheduler.Stop()
	scheduler.Stop()
	assert.False(t, scheduler.IsEnabled())
}

func TestSchedulerSetInterval(t *testing.T) {
	state := NewVectorizeState(nil)
	logger := log.New(io.Discard, "", 0)
	scheduler := NewScheduler(state, 30*time.Minute, logger)

	err := scheduler.SetInterval(15 * time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 15*time.Minute, scheduler.interval)
}

func TestSchedulerSetIntervalMinimum(t *testing.T) {
	state := NewVectorizeState(nil)
	logger := log.New(io.Discard, "", 0)
	scheduler := NewScheduler(state, 30*time.Minute, logger)

	// Set interval less than minimum
	err := scheduler.SetInterval(1 * time.Minute)
	require.NoError(t, err)
	assert.Equal(t, minSchedulerInterval, scheduler.interval)
}

func TestSchedulerSetIntervalWhileRunning(t *testing.T) {
	state := NewVectorizeState(nil)
	logger := log.New(io.Discard, "", 0)
	scheduler := NewScheduler(state, 5*time.Minute, logger)

	scheduler.SetRunFunc(func(ctx context.Context) error {
		return nil
	})

	err := scheduler.Start(context.Background())
	require.NoError(t, err)
	defer scheduler.Stop()

	// Update interval while running
	err = scheduler.SetInterval(10 * time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 10*time.Minute, scheduler.interval)
}

func TestSchedulerGetState(t *testing.T) {
	state := NewVectorizeState(nil)
	logger := log.New(io.Discard, "", 0)
	scheduler := NewScheduler(state, 10*time.Minute, logger)

	schedulerState := scheduler.GetState()

	assert.False(t, schedulerState.Enabled)
	assert.Equal(t, 10*time.Minute, schedulerState.Interval)
	assert.Zero(t, schedulerState.NextRunAt)
	assert.Zero(t, schedulerState.LastRunAt)
}

func TestSchedulerTickTriggersRunFunc(t *testing.T) {
	state := NewVectorizeState(nil)
	logger := log.New(io.Discard, "", 0)
	// Use a very short interval for testing
	scheduler := NewScheduler(state, 5*time.Minute, logger)

	var callCount int32
	scheduler.SetRunFunc(func(ctx context.Context) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	})

	// Manually trigger tick
	scheduler.mu.Lock()
	scheduler.enabled = true
	scheduler.mu.Unlock()

	ctx := context.Background()
	scheduler.tick(ctx)

	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))

	schedulerState := scheduler.GetState()
	assert.NotZero(t, schedulerState.LastRunAt)
}

func TestSchedulerSkipsWhenVectorizationRunning(t *testing.T) {
	state := NewVectorizeState(nil)
	logger := log.New(io.Discard, "", 0)
	scheduler := NewScheduler(state, 5*time.Minute, logger)

	var callCount int32
	scheduler.SetRunFunc(func(ctx context.Context) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	})

	// Start a vectorization run
	state.StartRun(10, false)

	// Enable scheduler manually
	scheduler.mu.Lock()
	scheduler.enabled = true
	scheduler.mu.Unlock()

	// Tick should skip because vectorization is running
	ctx := context.Background()
	scheduler.tick(ctx)

	assert.Equal(t, int32(0), atomic.LoadInt32(&callCount))
}

func TestSchedulerTickWhenDisabled(t *testing.T) {
	state := NewVectorizeState(nil)
	logger := log.New(io.Discard, "", 0)
	scheduler := NewScheduler(state, 5*time.Minute, logger)

	var callCount int32
	scheduler.SetRunFunc(func(ctx context.Context) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	})

	// Scheduler is not enabled
	ctx := context.Background()
	scheduler.tick(ctx)

	assert.Equal(t, int32(0), atomic.LoadInt32(&callCount))
}

func TestSchedulerTickWithNilRunFunc(t *testing.T) {
	state := NewVectorizeState(nil)
	logger := log.New(io.Discard, "", 0)
	scheduler := NewScheduler(state, 5*time.Minute, logger)

	// Enable scheduler without run func
	scheduler.mu.Lock()
	scheduler.enabled = true
	scheduler.mu.Unlock()

	// Should not panic
	ctx := context.Background()
	scheduler.tick(ctx)
}

func TestSchedulerContextCancellation(t *testing.T) {
	state := NewVectorizeState(nil)
	logger := log.New(io.Discard, "", 0)
	scheduler := NewScheduler(state, 5*time.Minute, logger)

	scheduler.SetRunFunc(func(ctx context.Context) error {
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	err := scheduler.Start(ctx)
	require.NoError(t, err)
	assert.True(t, scheduler.IsEnabled())

	// Cancel context
	cancel()

	// Give loop time to exit
	time.Sleep(10 * time.Millisecond)

	// Stop should still work
	scheduler.Stop()
	assert.False(t, scheduler.IsEnabled())
}

func TestSchedulerConcurrentAccess(t *testing.T) {
	state := NewVectorizeState(nil)
	logger := log.New(io.Discard, "", 0)
	scheduler := NewScheduler(state, 5*time.Minute, logger)

	scheduler.SetRunFunc(func(ctx context.Context) error {
		return nil
	})

	done := make(chan bool)

	// Concurrent start/stop
	go func() {
		for i := 0; i < 10; i++ {
			_ = scheduler.Start(context.Background())
			scheduler.Stop()
		}
		done <- true
	}()

	// Concurrent get state
	go func() {
		for i := 0; i < 50; i++ {
			_ = scheduler.GetState()
			_ = scheduler.IsEnabled()
		}
		done <- true
	}()

	// Concurrent set interval
	go func() {
		for i := 0; i < 20; i++ {
			_ = scheduler.SetInterval(time.Duration(5+i) * time.Minute)
		}
		done <- true
	}()

	<-done
	<-done
	<-done
}
