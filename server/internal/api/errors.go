// Package api implements the four HTTP routes exposed by the notification
// service: /healthz, POST /api/v1/notifications, the receiver SSE stream, and
// the receiver test endpoint. It also owns request middleware (request id,
// panic recovery, structured access logging, trusted-proxy client IP) and the
// unified error response format.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Error codes and their HTTP status mapping.
const (
	CodeInvalidRequest       = "invalid_request"
	CodeUnauthorized         = "unauthorized"
	CodeReceiverNotAllowed   = "receiver_not_allowed"
	CodeReceiverNotFound     = "receiver_not_found"
	CodeIdempotencyConflict  = "idempotency_conflict"
	CodePayloadTooLarge      = "payload_too_large"
	CodeRateLimited          = "rate_limited"
	CodeInternalError        = "internal_error"
	CodeDeliveryUnavailable  = "delivery_unavailable"
	CodeUnsupportedMediaType = "unsupported_media_type"
	CodeMethodNotAllowed     = "method_not_allowed"
)

var statusByCode = map[string]int{
	CodeInvalidRequest:       http.StatusBadRequest,
	CodeUnauthorized:         http.StatusUnauthorized,
	CodeReceiverNotAllowed:   http.StatusForbidden,
	CodeReceiverNotFound:     http.StatusNotFound,
	CodeIdempotencyConflict:  http.StatusConflict,
	CodePayloadTooLarge:      http.StatusRequestEntityTooLarge,
	CodeRateLimited:          http.StatusTooManyRequests,
	CodeInternalError:        http.StatusInternalServerError,
	CodeDeliveryUnavailable:  http.StatusServiceUnavailable,
	CodeUnsupportedMediaType: http.StatusUnsupportedMediaType,
	CodeMethodNotAllowed:     http.StatusMethodNotAllowed,
}

// apiError carries a stable error code plus a non-sensitive message.
type apiError struct {
	Code    string
	Message string
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

func errf(code, format string, args ...any) *apiError {
	return &apiError{Code: code, Message: fmt.Sprintf(format, args...)}
}

func errInternal() *apiError {
	return &apiError{Code: CodeInternalError, Message: "internal error"}
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// writeError writes the unified error JSON, records the outcome on the audit
// context, and never echoes tokens or bodies.
func (a *App) writeError(w http.ResponseWriter, r *http.Request, e *apiError) {
	status := statusByCode[e.Code]
	if status == 0 {
		status = http.StatusInternalServerError
	}
	setAudit(r, auditFields{statusCode: status, errorClass: e.Code})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	body := errorBody{}
	body.Error.Code = e.Code
	body.Error.Message = e.Message
	_ = json.NewEncoder(w).Encode(body)
}

// respond writes a JSON success body and records the status for audit.
func (a *App) respond(w http.ResponseWriter, r *http.Request, status int, payload any) {
	setAudit(r, auditFields{statusCode: status})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload != nil {
		_ = json.NewEncoder(w).Encode(payload)
	}
}
