// Package hub tracks the at-most-one live SSE connection per receiver and
// delivers formatted SSE payloads to it. New connections replace older ones for
// the same receiver_id by cancelling the previous connection's context.
package hub

import (
	"context"
	"sync"
	"time"
)

// connection binds a receiver to its delivery channel and cancellation func.
type connection struct {
	ctx    context.Context
	cancel context.CancelFunc
	ch     chan []byte
}

// Hub is safe for concurrent use.
type Hub struct {
	mu    sync.Mutex
	conns map[string]*connection
}

// New constructs an empty Hub.
func New() *Hub {
	return &Hub{conns: make(map[string]*connection)}
}

// Open registers a new connection for receiverID, cancelling and superseding
// any previous one. It returns:
//   - ch: the channel to read SSE payload bytes from
//   - replaced: a channel that is closed when this connection has been
//     superseded by a newer one (the handler should then stop)
//   - release: the handler MUST call this on exit; it only removes the entry
//     when it is still the current one, so a newer connection is never evicted
//     by a stale handler.
func (h *Hub) Open(receiverID string) (ch chan []byte, replaced <-chan struct{}, release func()) {
	ctx, cancel := context.WithCancel(context.Background())
	ch = make(chan []byte, 32)
	conn := &connection{ctx: ctx, cancel: cancel, ch: ch}

	h.mu.Lock()
	if old, ok := h.conns[receiverID]; ok {
		// Supersede the previous connection; its handler observes replaced close.
		old.cancel()
	}
	h.conns[receiverID] = conn
	h.mu.Unlock()

	release = func() {
		cancel()
		h.mu.Lock()
		if cur, ok := h.conns[receiverID]; ok && cur == conn {
			delete(h.conns, receiverID)
		}
		h.mu.Unlock()
	}
	return ch, ctx.Done(), release
}

// Send delivers payload to the current connection for receiverID. It returns
// true only if the payload was queued for an online connection. A slow or
// stalled consumer is given up to 2 seconds before the payload is considered
// undeliverable.
func (h *Hub) Send(receiverID string, payload []byte) bool {
	h.mu.Lock()
	conn, ok := h.conns[receiverID]
	h.mu.Unlock()
	if !ok {
		return false
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case conn.ch <- payload:
		return true
	case <-conn.ctx.Done():
		return false
	case <-timer.C:
		return false
	}
}

// IsOnline reports whether a live connection exists for receiverID.
func (h *Hub) IsOnline(receiverID string) bool {
	h.mu.Lock()
	_, ok := h.conns[receiverID]
	h.mu.Unlock()
	return ok
}
