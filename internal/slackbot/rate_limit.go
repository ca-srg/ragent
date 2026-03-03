package slackbot

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type RateLimiter struct {
	user    *scopedLimiter
	channel *scopedLimiter
	global  *rate.Limiter
}

type scopedLimiter struct {
	mu    sync.Mutex
	m     map[string]*rate.Limiter
	rate  rate.Limit
	burst int
}

func newScopedLimiter(perMinute int) *scopedLimiter {
	if perMinute <= 0 {
		perMinute = 60
	}
	return &scopedLimiter{
		m:     make(map[string]*rate.Limiter),
		rate:  rate.Limit(float64(perMinute) / 60.0),
		burst: perMinute,
	}
}

func (s *scopedLimiter) allow(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	lim, ok := s.m[key]
	if !ok {
		lim = rate.NewLimiter(s.rate, s.burst)
		s.m[key] = lim
	}
	return lim.Allow()
}

// NewRateLimiter constructs composite limiter with per-user/channel/global budgets
func NewRateLimiter(userPerMinute, channelPerMinute, globalPerMinute int) *RateLimiter {
	if globalPerMinute <= 0 {
		globalPerMinute = 100
	}
	return &RateLimiter{
		user:    newScopedLimiter(userPerMinute),
		channel: newScopedLimiter(channelPerMinute),
		global:  rate.NewLimiter(rate.Limit(float64(globalPerMinute)/60.0), globalPerMinute),
	}
}

func (r *RateLimiter) Allow(userID, channelID string) bool {
	if !r.global.Allow() {
		return false
	}
	if !r.user.allow(userID) {
		return false
	}
	if !r.channel.allow(channelID) {
		return false
	}
	return true
}

// Optional helper to wait with context (unused for now)
func (r *RateLimiter) WaitAll(timeout time.Duration) bool { return true }
