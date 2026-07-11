package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/makia9879/electricwave/internal/domain"
	"github.com/makia9879/electricwave/internal/idgen"
	"github.com/makia9879/electricwave/internal/store"
)

// maxBodyBytes is the overall request body cap (8 KiB). We read one extra byte
// so an over-limit body can be detected unambiguously.
const maxBodyBytes = domain.MaxBodyBytes

// backlogFullRetryAfter is the fixed Retry-After (seconds) returned with a 429
// backlog_full response. It gives the client a hint to wait for acks/expiry to
// free capacity.
const backlogFullRetryAfter = 60

type duplicateResponse struct {
	NotificationID string `json:"notification_id"`
	Status         string `json:"status"`
}

type createdResponse struct {
	NotificationID string `json:"notification_id"`
	Status         string `json:"status"`
	EventID        int64  `json:"event_id"`
	ExpiresAt      string `json:"expires_at"`
}

type backlogCountBody struct {
	Count int `json:"count"`
}

type queuedResponse struct {
	NotificationID string           `json:"notification_id"`
	Status         string           `json:"status"`
	EventID        int64            `json:"event_id"`
	ExpiresAt      string           `json:"expires_at"`
	Backlog        backlogCountBody `json:"backlog"`
}

// handleNotifications implements POST /api/v1/notifications.
func (a *App) handleNotifications(w http.ResponseWriter, r *http.Request) {
	setAudit(r, auditFields{provider: a.cfg.DeliveryProvider})

	// 1. Content-Type must be application/json (charset optional).
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		a.writeError(w, r, errf(CodeUnsupportedMediaType, "Content-Type must be application/json"))
		return
	}

	// 2. Per source-IP rate limit (outermost abuse control; covers auth failures).
	ip := a.clientIP(r)
	if !a.ipLimiter.Allow(ip) {
		retry := a.ipLimiter.RetryAfterSeconds(ip)
		w.Header().Set("Retry-After", itoa(retry))
		a.writeError(w, r, errf(CodeRateLimited, "rate limit exceeded"))
		return
	}

	// 3. Authenticate the webhook access token.
	raw, ok := bearerToken(r)
	if !ok {
		a.writeError(w, r, &apiError{Code: CodeUnauthorized, Message: "missing or invalid Authorization"})
		return
	}
	tok, err := a.checker.Authenticate(raw)
	if err != nil {
		a.writeError(w, r, &apiError{Code: CodeUnauthorized, Message: "missing or invalid Authorization"})
		return
	}
	setAudit(r, auditFields{tokenID: tok.TokenID})

	// 4. Per-token rate limit.
	if !a.tokenLimiter.Allow(tok.TokenID) {
		retry := a.tokenLimiter.RetryAfterSeconds(tok.TokenID)
		w.Header().Set("Retry-After", itoa(retry))
		a.writeError(w, r, errf(CodeRateLimited, "rate limit exceeded"))
		return
	}

	// 5. Read body with an 8 KiB cap.
	body, perr := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if perr != nil {
		a.writeError(w, r, errf(CodeInvalidRequest, "could not read request body"))
		return
	}
	if len(body) > maxBodyBytes {
		a.writeError(w, r, errf(CodePayloadTooLarge, "request body exceeds 8 KiB"))
		return
	}

	// 6. Decode JSON, rejecting unknown fields and type mismatches.
	var req domain.NotificationRequest
	dec := json.NewDecoder(newBytesReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		a.writeError(w, r, errf(CodeInvalidRequest, "invalid JSON body"))
		return
	}
	if dec.More() {
		// Multiple JSON values are not allowed.
		a.writeError(w, r, errf(CodeInvalidRequest, "invalid JSON body"))
		return
	}

	// 7. Field validation.
	vn, verr := domain.Validate(&req)
	if verr != nil {
		a.writeError(w, r, &apiError{Code: CodeInvalidRequest, Message: verr.Error()})
		return
	}
	setAudit(r, auditFields{receiverID: vn.ReceiverID})

	// 8. Per-receiver rate limit (applied before lookup to throttle enumeration).
	if !a.receiverLimiter.Allow(vn.ReceiverID) {
		retry := a.receiverLimiter.RetryAfterSeconds(vn.ReceiverID)
		w.Header().Set("Retry-After", itoa(retry))
		a.writeError(w, r, errf(CodeRateLimited, "rate limit exceeded"))
		return
	}

	// 9. Receiver lookup + status check.
	recv, err := a.store.GetReceiver(r.Context(), vn.ReceiverID)
	if err != nil {
		if isNotFound(err) {
			a.writeError(w, r, errf(CodeReceiverNotFound, "receiver does not exist"))
			return
		}
		a.log.Error("receiver lookup failed", "request_id", auditFrom(r).requestID, "err", err.Error())
		a.writeError(w, r, errInternal())
		return
	}
	if !recv.Allowlisted || !recv.Enabled || recv.Revoked {
		a.writeError(w, r, errf(CodeReceiverNotAllowed, "receiver is not allowed"))
		return
	}

	contentHash := domain.ContentHash(vn)

	// 10. Idempotency pre-check (a known duplicate returns 200 even if the
	// receiver is currently offline, since it was already accepted).
	if outcome, id, err := a.store.CheckIdempotency(r.Context(), tok.TokenID, vn.ReceiverID, vn.IdempotencyKey, contentHash, idempotencyWindow); err != nil {
		a.log.Error("idempotency check failed", "request_id", auditFrom(r).requestID, "err", err.Error())
		a.writeError(w, r, errInternal())
		return
	} else if outcome == store.OutcomeDuplicate {
		a.respond(w, r, http.StatusOK, duplicateResponse{NotificationID: id, Status: "duplicate"})
		return
	} else if outcome == store.OutcomeConflict {
		a.writeError(w, r, errf(CodeIdempotencyConflict, "idempotency key conflicts with existing content"))
		return
	}

	// 11. Persist (transactional idempotency re-check, capacity guard, event_id
	// allocation). No row is created when the receiver backlog is full (§5, §6).
	notifID := idgen.NotificationID()
	now := time.Now()
	expiresAt := now.Add(time.Duration(vn.TTLSeconds) * time.Second)
	n := store.Notification{
		NotificationID: notifID,
		TokenID:        tok.TokenID,
		ReceiverID:     vn.ReceiverID,
		IdempotencyKey: vn.IdempotencyKey,
		ContentHash:    contentHash,
		Title:          vn.Title,
		Body:           vn.Body,
		Priority:       vn.Priority,
		GroupKey:       vn.GroupKey,
		DataJSON:       string(vn.DataJSON),
		TTLSeconds:     vn.TTLSeconds,
		ExpiresAt:      expiresAt,
		Status:         "accepted",
		CreatedAt:      now,
	}
	outcome, storedID, eventID, err := a.store.CreateNotification(r.Context(), n, idempotencyWindow, a.cfg.BacklogMaxPerReceiver)
	if err != nil {
		a.log.Error("create notification failed", "request_id", auditFrom(r).requestID, "err", err.Error())
		a.writeError(w, r, errInternal())
		return
	}
	switch outcome {
	case store.OutcomeDuplicate:
		a.respond(w, r, http.StatusOK, duplicateResponse{NotificationID: storedID, Status: "duplicate"})
		return
	case store.OutcomeConflict:
		a.writeError(w, r, errf(CodeIdempotencyConflict, "idempotency key conflicts with existing content"))
		return
	case store.OutcomeBacklogFull:
		w.Header().Set("Retry-After", itoa(backlogFullRetryAfter))
		a.writeError(w, r, errf(CodeBacklogFull, "receiver backlog is full"))
		return
	}

	// 12. Attempt real-time delivery. When the receiver is online and hub.Send
	// queues the event, mark sent and return 201. Otherwise mark queued and
	// return 202 (§5).
	if a.hub.IsOnline(vn.ReceiverID) {
		payload := formatNotificationEvent(eventID, notifID, vn.Title, vn.Body, vn.Priority, vn.GroupKey, vn.DataJSON, expiresAt)
		if a.hub.Send(vn.ReceiverID, payload) {
			if err := a.store.MarkSent(r.Context(), notifID); err != nil {
				a.log.Warn("mark sent failed", "notification_id", notifID, "err", err.Error())
			}
			a.respond(w, r, http.StatusCreated, createdResponse{
				NotificationID: notifID,
				Status:         "accepted",
				EventID:        eventID,
				ExpiresAt:      expiresAt.UTC().Format(time.RFC3339),
			})
			return
		}
		a.log.Warn("delivery failed after accept, queuing",
			"notification_id", notifID, "receiver_id", vn.ReceiverID, "event_id", eventID)
	}

	// Receiver offline or hub.Send did not deliver → queue for replay.
	if err := a.store.MarkQueued(r.Context(), notifID); err != nil {
		a.log.Warn("mark queued failed", "notification_id", notifID, "err", err.Error())
	}
	count, _ := a.store.BacklogCount(r.Context(), vn.ReceiverID, time.Now())
	a.respond(w, r, http.StatusAccepted, queuedResponse{
		NotificationID: notifID,
		Status:         "queued",
		EventID:        eventID,
		ExpiresAt:      expiresAt.UTC().Format(time.RFC3339),
		Backlog:        backlogCountBody{Count: count},
	})
}
