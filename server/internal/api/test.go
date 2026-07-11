package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/makia9879/electricwave/internal/auth"
	"github.com/makia9879/electricwave/internal/idgen"
	"github.com/makia9879/electricwave/internal/store"
)

// handleTest implements POST /api/v1/receivers/{receiver_id}/test.
//
// It authenticates the receiver identity token (same as the stream) and, when
// the receiver is online, delivers a fixed test notification event. The test
// notification is persisted so it participates in the event_id sequence and the
// reliable-delivery state machine like any webhook notification.
func (a *App) handleTest(w http.ResponseWriter, r *http.Request) {
	receiverID := r.PathValue("receiver_id")
	setAudit(r, auditFields{receiverID: receiverID, provider: a.cfg.DeliveryProvider})

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

	// The test endpoint verifies the live SSE path; it requires an online
	// receiver and does not queue.
	if !a.hub.IsOnline(receiverID) {
		a.writeError(w, r, errf(CodeDeliveryUnavailable, "receiver is offline"))
		return
	}

	notifID := idgen.NotificationID()
	now := time.Now()
	const testTTL = 3600
	expiresAt := now.Add(testTTL * time.Second)
	data, _ := json.Marshal(map[string]bool{"test": true})
	n := store.Notification{
		NotificationID: notifID,
		TokenID:        "__test__",
		ReceiverID:     receiverID,
		Title:          "测试通知",
		Body:           "这是一条来自服务端的测试通知",
		Priority:       "normal",
		DataJSON:       string(data),
		TTLSeconds:     testTTL,
		ExpiresAt:      expiresAt,
		Status:         "accepted",
		CreatedAt:      now,
	}
	outcome, _, eventID, err := a.store.CreateNotification(r.Context(), n, idempotencyWindow, a.cfg.BacklogMaxPerReceiver)
	if err != nil {
		a.writeError(w, r, errInternal())
		return
	}
	if outcome == store.OutcomeBacklogFull {
		w.Header().Set("Retry-After", itoa(backlogFullRetryAfter))
		a.writeError(w, r, errf(CodeBacklogFull, "receiver backlog is full"))
		return
	}

	payload := formatNotificationEvent(eventID, notifID, n.Title, n.Body, "normal", "", data, expiresAt)
	if !a.hub.Send(receiverID, payload) {
		_ = a.store.MarkQueued(r.Context(), notifID)
		a.writeError(w, r, errf(CodeDeliveryUnavailable, "receiver is offline"))
		return
	}
	if err := a.store.MarkSent(r.Context(), notifID); err != nil {
		a.log.Warn("test mark sent failed", "notification_id", notifID, "err", err.Error())
	}
	a.respond(w, r, http.StatusOK, map[string]string{"status": "test_sent", "notification_id": notifID})
}
