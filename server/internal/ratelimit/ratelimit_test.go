package ratelimit

import (
	"testing"
	"time"
)

func TestAllowUnderLimit(t *testing.T) {
	l := New(60)
	for i := 0; i < 60; i++ {
		if !l.Allow("k") {
			t.Fatalf("expected allow at %d", i)
		}
	}
	// 61st immediately after burst should be denied (bucket exhausted).
	if l.Allow("k") {
		t.Fatal("expected deny after burst exhausted")
	}
}

func TestRetryAfterPositive(t *testing.T) {
	l := New(60)
	for i := 0; i < 60; i++ {
		l.Allow("k")
	}
	if l.Allow("k") {
		t.Fatal("expected deny")
	}
	ra := l.RetryAfterSeconds("k")
	if ra < 1 {
		t.Fatalf("retry-after should be >=1, got %d", ra)
	}
}

func TestIndependentKeys(t *testing.T) {
	l := New(1)
	if !l.Allow("a") {
		t.Fatal("a should be allowed")
	}
	if l.Allow("a") {
		t.Fatal("a second hit should be denied")
	}
	// Different key has its own bucket.
	if !l.Allow("b") {
		t.Fatal("b should be allowed independently")
	}
}

func TestCleanup(t *testing.T) {
	l := New(10)
	l.idleExpiry = time.Nanosecond
	l.Allow("k")
	// lastUsed is ~now; cleaning up with a future timestamp past the idle
	// expiry should evict the entry.
	l.Cleanup(time.Now().Add(time.Hour))
	// After cleanup, the bucket for "k" is gone, so it can be allowed again.
	if !l.Allow("k") {
		t.Fatal("expected allow after cleanup re-created bucket")
	}
}
