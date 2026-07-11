package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/makia9879/electricwave/internal/auth"
	"github.com/makia9879/electricwave/internal/store"
)

// handleStream implements GET /api/v1/receivers/{receiver_id}/stream.
//
// It authenticates the receiver identity token, verifies the receiver exists
// and is allowed, then opens a long-lived text/event-stream connection. On
// (re)connect the client sends Last-Event-ID (cursor) and X-Receiver-Ack
// (cleanup) headers. The handler applies the ack, emits an info control event,
// optionally a backlog_gap event, replays the pending backlog, then enters the
// real-time push loop (0007-integration-contract §4).
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

	// Parse reconnect headers (§3). Missing or non-positive → 0.
	lastEventID := parseEventIDHeader(r.Header, "Last-Event-ID")
	ackEventID := parseEventIDHeader(r.Header, "X-Receiver-Ack")

	// Apply ack (idempotent) before the connection is opened (§4.3).
	if ackEventID > 0 {
		if _, err := a.store.ApplyAck(r.Context(), receiverID, ackEventID); err != nil {
			a.log.Warn("apply ack failed",
				"receiver_id", receiverID, "ack_event_id", ackEventID, "err", err.Error())
		}
	}

	// Determine replay cursor (§4.4): Last-Event-ID wins; otherwise the ack.
	cursor := lastEventID
	if cursor <= 0 {
		cursor = ackEventID
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

	now := time.Now()

	// Backlog summary drives both the info event and gap detection.
	sum, err := a.store.BacklogSummary(r.Context(), receiverID, now)
	if err != nil {
		a.log.Warn("backlog summary failed",
			"receiver_id", receiverID, "err", err.Error())
		sum = store.BacklogSummary{} // proceed with empty info
	}

	// 1) info event (§4.7).
	if _, err := w.Write(formatInfoEvent(sum.AckedEventID, sum.OldestUnackedEventID, sum.NewestEventID, sum.Count, sum.OldestUnackedAcceptedAt)); err != nil {
		return
	}
	flusher.Flush()

	// 2) backlog_gap event when the cursor leaves an irrecoverable hole (§4.6).
	if cursor >= 1 && sum.OldestUnackedEventID > 0 && cursor+1 < sum.OldestUnackedEventID {
		if _, err := w.Write(formatBacklogGapEvent(cursor+1, sum.OldestUnackedEventID-1, "retention_exceeded")); err != nil {
			return
		}
		flusher.Flush()
	}

	// 3) Replay set: queued/sent rows after the cursor, ascending event_id
	// (§4.5, §4.7). Each replayed event transitions to sent.
	replay, err := a.store.ReplaySet(r.Context(), receiverID, cursor, now)
	if err != nil {
		a.log.Warn("replay query failed",
			"receiver_id", receiverID, "err", err.Error())
	}
	for _, n := range replay {
		payload := formatNotificationEvent(n.EventID, n.NotificationID, n.Title, n.Body, n.Priority, n.GroupKey, json.RawMessage(n.DataJSON), n.ExpiresAt)
		if _, err := w.Write(payload); err != nil {
			return
		}
		flusher.Flush()
		if err := a.store.MarkSent(r.Context(), n.NotificationID); err != nil {
			a.log.Warn("replay mark sent failed",
				"notification_id", n.NotificationID, "event_id", n.EventID, "err", err.Error())
		}
	}

	// 4) Real-time push loop.
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

// parseEventIDHeader parses a decimal event-id header (Last-Event-ID or
// X-Receiver-Ack). A missing, non-numeric, or non-positive value yields 0.
func parseEventIDHeader(h http.Header, key string) int64 {
	v := strings.TrimSpace(h.Get(key))
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
