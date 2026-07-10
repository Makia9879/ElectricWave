package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/makia9879/makia-notice/internal/auth"
	"github.com/makia9879/makia-notice/internal/idgen"
)

// handleTest implements POST /api/v1/receivers/{receiver_id}/test.
//
// It authenticates the receiver identity token (same as the stream) and, when
// the receiver is online, delivers a fixed test notification event.
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

	if !a.hub.IsOnline(receiverID) {
		a.writeError(w, r, errf(CodeDeliveryUnavailable, "receiver is offline"))
		return
	}

	notifID := idgen.NotificationID()
	data, _ := json.Marshal(map[string]bool{"test": true})
	payload := formatNotificationEvent(
		notifID,
		"测试通知",
		"这是一条来自服务端的测试通知",
		"normal",
		"",
		data,
		3600*time.Second,
	)
	if !a.hub.Send(receiverID, payload) {
		a.writeError(w, r, errf(CodeDeliveryUnavailable, "receiver is offline"))
		return
	}
	a.respond(w, r, http.StatusOK, map[string]string{"status": "test_sent", "notification_id": notifID})
}
