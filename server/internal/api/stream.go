package api

import (
	"net/http"
	"time"

	"github.com/makia9879/makia-notice/internal/auth"
)

// handleStream implements GET /api/v1/receivers/{receiver_id}/stream.
//
// It authenticates the receiver identity token, verifies the receiver exists
// and is allowed, then opens a long-lived text/event-stream connection. A newer
// connection for the same receiver supersedes (cancels) the previous one. The
// handler emits an SSE comment heartbeat every SSE_HEARTBEAT_INTERVAL_SECONDS
// and notification events as they arrive via the hub.
func (a *App) handleStream(w http.ResponseWriter, r *http.Request) {
	receiverID := r.PathValue("receiver_id")
	setAudit(r, auditFields{receiverID: receiverID, provider: a.cfg.DeliveryProvider})

	flusher, ok := w.(http.Flusher)
	if !ok {
		a.writeError(w, r, errInternal())
		return
	}

	// Authenticate identity token.
	raw, ok := bearerToken(r)
	if !ok {
		a.writeError(w, r, &apiError{Code: CodeUnauthorized, Message: "missing or invalid Authorization"})
		return
	}
	recv, err := a.store.GetReceiver(r.Context(), receiverID)
	if err != nil {
		if isNotFound(err) {
			a.writeError(w, r, errf(CodeReceiverNotFound, "receiver does not exist"))
			return
		}
		a.writeError(w, r, errInternal())
		return
	}
	if !recv.Allowlisted || !recv.Enabled || recv.Revoked {
		a.writeError(w, r, errf(CodeReceiverNotAllowed, "receiver is not allowed"))
		return
	}
	if !auth.Verify(raw, recv.IdentityTokenHash, a.cfg.TokenHashPepper) {
		a.writeError(w, r, &apiError{Code: CodeUnauthorized, Message: "missing or invalid Authorization"})
		return
	}

	// Open the connection (superseding any previous one).
	ch, replaced, release := a.hub.Open(receiverID)
	defer release()

	// Write response headers before the first byte.
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	interval := a.cfg.SSEHeartbeatInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	heartbeat := time.NewTicker(interval)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-replaced:
			return
		case <-heartbeat.C:
			if _, err := w.Write(formatHeartbeat()); err != nil {
				return
			}
			flusher.Flush()
		case payload := <-ch:
			if _, err := w.Write(payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
