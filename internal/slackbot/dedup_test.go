package slackbot

import (
	"sync"
	"testing"
	"time"
)

func TestEventDedup_FirstSeenReturnsFalse(t *testing.T) {
	d := newEventDedup(time.Minute, 16)
	if d.markSeen("evt-1") {
		t.Fatalf("first occurrence should not be reported as seen")
	}
}

func TestEventDedup_SecondSeenReturnsTrue(t *testing.T) {
	d := newEventDedup(time.Minute, 16)
	if d.markSeen("evt-1") {
		t.Fatalf("first occurrence should not be reported as seen")
	}
	if !d.markSeen("evt-1") {
		t.Fatalf("duplicate occurrence should be reported as seen")
	}
}

func TestEventDedup_EmptyKeyIsNotDeduped(t *testing.T) {
	d := newEventDedup(time.Minute, 16)
	if d.markSeen("") {
		t.Fatalf("empty key must never be considered duplicate")
	}
	if d.markSeen("") {
		t.Fatalf("empty key must remain non-duplicate on repeat calls")
	}
}

func TestEventDedup_NilReceiverIsSafe(t *testing.T) {
	var d *eventDedup
	if d.markSeen("anything") {
		t.Fatalf("nil dedup must report not-seen, not crash")
	}
}

func TestEventDedup_ExpiresAfterTTL(t *testing.T) {
	d := newEventDedup(20*time.Millisecond, 16)
	if d.markSeen("evt") {
		t.Fatalf("first occurrence should not be seen")
	}
	time.Sleep(40 * time.Millisecond)
	if d.markSeen("evt") {
		t.Fatalf("entry should have expired after TTL")
	}
}

func TestEventDedup_RespectsMaxSize(t *testing.T) {
	d := newEventDedup(time.Minute, 8)
	for i := 0; i < 32; i++ {
		key := string(rune('a'+i%26)) + string(rune('0'+i%10))
		d.markSeen(key)
	}
	d.mu.Lock()
	size := len(d.seen)
	d.mu.Unlock()
	if size > 8 {
		t.Fatalf("dedup map exceeded maxSize: got %d, want <=8", size)
	}
}

func TestEventDedup_ConcurrentSafe(t *testing.T) {
	d := newEventDedup(time.Minute, 1024)
	var wg sync.WaitGroup
	const workers = 16
	const perWorker = 100
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				_ = d.markSeen("shared-key")
			}
		}(w)
	}
	wg.Wait()
	if !d.markSeen("shared-key") {
		t.Fatalf("after concurrent inserts the shared key should be marked seen")
	}
}
