package slackbot

import (
	"sync"
	"time"
)

// eventDedup keeps a TTL-bounded record of recently seen event keys so that
// duplicate Slack event deliveries (e.g. Events API retries when an Ack is
// not observed in time, or RTM reconnect backlogs) do not trigger the bot to
// respond multiple times to the same user message.
type eventDedup struct {
	mu      sync.Mutex
	seen    map[string]time.Time
	ttl     time.Duration
	maxSize int
}

// newEventDedup creates a dedup tracker. ttl controls how long a key is
// considered seen; maxSize bounds the map to avoid unbounded growth.
func newEventDedup(ttl time.Duration, maxSize int) *eventDedup {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if maxSize <= 0 {
		maxSize = 4096
	}
	return &eventDedup{
		seen:    make(map[string]time.Time),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

// markSeen returns true if the key was already seen within the TTL window.
// Otherwise it records the key and returns false.
func (d *eventDedup) markSeen(key string) bool {
	if d == nil || key == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	if ts, ok := d.seen[key]; ok && now.Sub(ts) < d.ttl {
		// refresh ttl so a hot key keeps being deduped
		d.seen[key] = now
		return true
	}

	// opportunistic GC: prune expired entries when we approach maxSize
	if len(d.seen) >= d.maxSize {
		for k, ts := range d.seen {
			if now.Sub(ts) >= d.ttl {
				delete(d.seen, k)
			}
		}
		// if still too large after GC, drop arbitrary oldest-ish entries
		if len(d.seen) >= d.maxSize {
			for k := range d.seen {
				delete(d.seen, k)
				if len(d.seen) < d.maxSize {
					break
				}
			}
		}
	}

	d.seen[key] = now
	return false
}
