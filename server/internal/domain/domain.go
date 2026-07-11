// Package domain defines the core data types, request validation and the
// stable content hash used for idempotency.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Field length limits from the endpoint contract.
const (
	MaxReceiverIDRunes    = 256
	MaxTitleRunes         = 128
	MaxBodyRunes          = 1024
	MaxIdempotencyKeyRunes = 128
	MaxGroupKeyRunes      = 64
	MaxDataBytes          = 4096
	MaxBodyBytes          = 8 * 1024 // 8 KiB overall request body cap
)

// Priority values.
const (
	PriorityLow    = "low"
	PriorityNormal = "normal"
	PriorityHigh   = "high"
)

// NotificationRequest is the decoded (not yet validated) webhook payload.
// receiver_id and data are kept as RawMessage so that type mistakes (arrays,
// objects, numbers) can be rejected explicitly.
type NotificationRequest struct {
	ReceiverID     json.RawMessage `json:"receiver_id"`
	Title          *string         `json:"title"`
	Body           *string         `json:"body"`
	IdempotencyKey *string         `json:"idempotency_key"`
	Priority       *string         `json:"priority"`
	TTLSeconds     *int            `json:"ttl_seconds"`
	GroupKey       *string         `json:"group_key"`
	Icon           *string         `json:"icon"`
	Data           json.RawMessage `json:"data"`
}

// ValidatedNotification is the normalized, validated view of a request.
type ValidatedNotification struct {
	ReceiverID    string
	Title         string
	Body          string
	IdempotencyKey string // "" when absent
	Priority      string
	TTLSeconds    int
	GroupKey      string
	Icon          string
	DataJSON      json.RawMessage // canonical compact JSON object (never null)
}

// ValidationError describes a single 400-class field problem.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func verr(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// runeCount counts Unicode code points.
func runeCount(s string) int { return len([]rune(s)) }

// Validate normalizes and validates the decoded request. It returns the
// validated view or the first ValidationError encountered.
func Validate(req *NotificationRequest) (*ValidatedNotification, error) {
	v := &ValidatedNotification{
		Priority:   PriorityNormal,
		TTLSeconds: 3600,
		Icon:       "default",
		DataJSON:   json.RawMessage("{}"),
	}

	// receiver_id: must be a non-empty string, never array/object/null/number.
	if len(req.ReceiverID) == 0 || string(req.ReceiverID) == "null" {
		return nil, verr("receiver_id is required")
	}
	var rid string
	if err := json.Unmarshal(req.ReceiverID, &rid); err != nil {
		return nil, verr("receiver_id must be a string")
	}
	if rid == "" {
		return nil, verr("receiver_id must not be empty")
	}
	if runeCount(rid) > MaxReceiverIDRunes {
		return nil, verr("receiver_id is too long")
	}
	v.ReceiverID = rid

	// title
	if req.Title == nil {
		return nil, verr("title is required")
	}
	t := *req.Title
	if runeCount(t) < 1 || runeCount(t) > MaxTitleRunes {
		return nil, verr("title must be between 1 and %d characters", MaxTitleRunes)
	}
	v.Title = t

	// body
	if req.Body == nil {
		return nil, verr("body is required")
	}
	b := *req.Body
	if runeCount(b) < 1 || runeCount(b) > MaxBodyRunes {
		return nil, verr("body must be between 1 and %d characters", MaxBodyRunes)
	}
	v.Body = b

	// idempotency_key
	if req.IdempotencyKey != nil {
		ik := *req.IdempotencyKey
		if ik != "" && runeCount(ik) > MaxIdempotencyKeyRunes {
			return nil, verr("idempotency_key is too long")
		}
		v.IdempotencyKey = ik
	}

	// priority
	if req.Priority != nil {
		p := *req.Priority
		switch p {
		case PriorityLow, PriorityNormal, PriorityHigh:
			v.Priority = p
		default:
			return nil, verr("priority must be one of low, normal, high")
		}
	}

	// ttl_seconds
	if req.TTLSeconds != nil {
		ttl := *req.TTLSeconds
		if ttl < 60 || ttl > 86400 {
			return nil, verr("ttl_seconds must be between 60 and 86400")
		}
		v.TTLSeconds = ttl
	}

	// group_key
	if req.GroupKey != nil {
		gk := *req.GroupKey
		if runeCount(gk) > MaxGroupKeyRunes {
			return nil, verr("group_key is too long")
		}
		v.GroupKey = gk
	}

	// icon
	if req.Icon != nil {
		ic := *req.Icon
		if ic != "default" {
			return nil, verr("icon must be 'default'")
		}
		v.Icon = ic
	}

	// data: must be a JSON object (or absent). Canonicalize and size-check.
	if len(req.Data) > 0 && string(req.Data) != "null" {
		var obj map[string]any
		if err := json.Unmarshal(req.Data, &obj); err != nil {
			return nil, verr("data must be a JSON object")
		}
		canonical, err := json.Marshal(obj)
		if err != nil {
			return nil, verr("data could not be encoded")
		}
		if len(canonical) > MaxDataBytes {
			return nil, verr("data exceeds %d bytes", MaxDataBytes)
		}
		v.DataJSON = canonical
	}

	return v, nil
}

// contentPayload mirrors exactly the fields included in the content hash.
// Field declaration order defines the stable JSON key order. The data field is
// a canonicalized object so key ordering does not affect the digest.
type contentPayload struct {
	Title    string          `json:"title"`
	Body     string          `json:"body"`
	Priority string          `json:"priority"`
	GroupKey string          `json:"group_key"`
	Data     json.RawMessage `json:"data"`
}

// ContentHash returns the SHA-256 hex digest of the stable JSON encoding of
// {title, body, priority, group_key, data}. ttl_seconds, idempotency_key and
// receiver_id are intentionally excluded.
func ContentHash(v *ValidatedNotification) string {
	p := contentPayload{
		Title:    v.Title,
		Body:     v.Body,
		Priority: v.Priority,
		GroupKey: v.GroupKey,
		Data:     v.DataJSON,
	}
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
