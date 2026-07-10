// Package ratelimit provides keyed token-bucket rate limiters used to throttle
// webhook traffic per webhook token, per source IP and per receiver.
package ratelimit

import (
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// entry pairs a limiter with its last-use timestamp for idle eviction.
type entry struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// KeyedLimiter is a map of key -> *rate.Limiter with the same per-minute budget
// for every key. Idle entries are evicted by Cleanup to bound memory.
type KeyedLimiter struct {
	mu          sync.Mutex
	entries     map[string]*entry
	ratePerSec  rate.Limit
	burst       int
	idleExpiry  time.Duration
}

// New creates a limiter allowing eventsPerMinute (burst == eventsPerMinute,
// refilled smoothly across the minute).
func New(eventsPerMinute int) *KeyedLimiter {
	if eventsPerMinute <= 0 {
		eventsPerMinute = 1
	}
	return &KeyedLimiter{
		entries:    make(map[string]*entry),
		ratePerSec: rate.Limit(float64(eventsPerMinute) / 60.0),
		burst:      eventsPerMinute,
		idleExpiry: 15 * time.Minute,
	}
}

// Allow reports whether key may proceed. It lazily creates the per-key bucket.
func (l *KeyedLimiter) Allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	e, ok := l.entries[key]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(l.ratePerSec, l.burst)}
		l.entries[key] = e
	}
	e.lastUsed = now
	l.mu.Unlock()
	return e.limiter.Allow()
}

// RetryAfterSeconds estimates the whole seconds to wait until the bucket for key
// would have at least one token. Returns at least 1. It does not consume a
// token.
func (l *KeyedLimiter) RetryAfterSeconds(key string) int {
	l.mu.Lock()
	e, ok := l.entries[key]
	l.mu.Unlock()
	if !ok {
		return 1
	}
	if e.limiter.Limit() <= 0 {
		return 1
	}
	tokens := e.limiter.Tokens()
	if tokens >= 1 {
		return 1
	}
	needed := 1 - tokens
	secs := needed / float64(e.limiter.Limit())
	return clampAtLeast1(int(math.Ceil(secs)))
}

// Cleanup removes entries not used since before the idle expiry.
func (l *KeyedLimiter) Cleanup(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, e := range l.entries {
		if now.Sub(e.lastUsed) > l.idleExpiry {
			delete(l.entries, k)
		}
	}
}

func clampAtLeast1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
