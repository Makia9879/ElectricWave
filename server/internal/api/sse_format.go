package api

import (
	"encoding/json"
	"time"
)

// streamEvent is the JSON object emitted on the SSE data: line for a
// notification. Field order is fixed and stable.
type streamEvent struct {
	Type           string          `json:"type"`
	NotificationID string          `json:"notification_id"`
	Title          string          `json:"title"`
	Body           string          `json:"body"`
	Priority       string          `json:"priority"`
	GroupKey       string          `json:"group_key,omitempty"`
	Data           json.RawMessage `json:"data"`
	ExpiresAt      string          `json:"expires_at"`
}

// formatNotificationEvent builds the complete SSE block for a notification:
//
//	event: notification
//	data: {json}
//	<blank line>
func formatNotificationEvent(notifID, title, body, priority, groupKey string, dataJSON json.RawMessage, ttl time.Duration) []byte {
	ev := streamEvent{
		Type:           "notification",
		NotificationID: notifID,
		Title:          title,
		Body:           body,
		Priority:       priority,
		GroupKey:       groupKey,
		Data:           dataJSON,
		ExpiresAt:      time.Now().UTC().Add(ttl).Format(time.RFC3339),
	}
	js, _ := json.Marshal(ev)
	// Trailing blank line is required to terminate the SSE event.
	return []byte("event: notification\ndata: " + string(js) + "\n\n")
}

// formatHeartbeat returns the SSE comment heartbeat block.
func formatHeartbeat() []byte { return []byte(": heartbeat\n\n") }
