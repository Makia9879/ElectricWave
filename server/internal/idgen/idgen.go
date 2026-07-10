// Package idgen produces monotonic, sortable identifiers for notifications
// and requests. IDs are ULID-based to guarantee uniqueness even when several
// are generated within the same millisecond.
package idgen

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	mu    sync.Mutex
	entropy = ulid.Monotonic(rand.Reader, 0)
)

// NotificationID returns a fresh "ntf_"-prefixed ULID.
func NotificationID() string {
	mu.Lock()
	defer mu.Unlock()
	return "ntf_" + ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
}

// RequestID returns a fresh "req_"-prefixed ULID used for tracing and audit.
func RequestID() string {
	mu.Lock()
	defer mu.Unlock()
	return "req_" + ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
}
