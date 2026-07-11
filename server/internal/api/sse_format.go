package api

import (
	"encoding/json"
	"strconv"
	"time"
)

// streamEvent is the JSON object emitted on the SSE data: line for a
// notification. Field order is fixed and stable.
type streamEvent struct {
	Type           string          `json:"type"`
	NotificationID string          `json:"notification_id"`
	EventID        int64           `json:"event_id"`
	Title          string          `json:"title"`
	Body           string          `json:"body"`
	Priority       string          `json:"priority"`
	GroupKey       string          `json:"group_key,omitempty"`
	Data           json.RawMessage `json:"data"`
	ExpiresAt      string          `json:"expires_at"`
}

// formatNotificationEvent builds the complete SSE block for a notification
// (§2.1). The event_id appears in both the id: line and the JSON event_id field.
//
//	id: 42
//	event: notification
//	data: {json}
//	<blank line>
func formatNotificationEvent(eventID int64, notifID, title, body, priority, groupKey string, dataJSON json.RawMessage, expiresAt time.Time) []byte {
	ev := streamEvent{
		Type:           "notification",
		NotificationID: notifID,
		EventID:        eventID,
		Title:          title,
		Body:           body,
		Priority:       priority,
		GroupKey:       groupKey,
		Data:           dataJSON,
		ExpiresAt:      expiresAt.UTC().Format(time.RFC3339),
	}
	js, _ := json.Marshal(ev)
	// Trailing blank line is required to terminate the SSE event.
	return []byte("id: " + strconv.FormatInt(eventID, 10) + "\nevent: notification\ndata: " + string(js) + "\n\n")
}

// infoPayload mirrors the JSON emitted in the info control event (§2.2). A nil
// pointer renders as JSON null.
type infoPayload struct {
	Type                    string  `json:"type"`
	AckedEventID            *int64  `json:"acked_event_id"`
	OldestUnackedEventID    *int64  `json:"oldest_unacked_event_id"`
	NewestEventID           *int64  `json:"newest_event_id"`
	BacklogCount            int     `json:"backlog_count"`
	OldestUnackedAcceptedAt *string `json:"oldest_unacked_accepted_at"`
}

// formatInfoEvent builds the info control event emitted once at stream
// handshake after the replay cursor is determined (§2.2, §4.7).
// oldestUnackedAcceptedAt is the RFC3339 timestamp of the oldest backlog item
// (the "最老积压时间" diagnostic, §10.4); empty renders as null.
func formatInfoEvent(ackedEventID, oldestUnackedEventID, newestEventID int64, backlogCount int, oldestUnackedAcceptedAt string) []byte {
	p := infoPayload{Type: "info", BacklogCount: backlogCount}
	if ackedEventID > 0 {
		v := ackedEventID
		p.AckedEventID = &v
	}
	if oldestUnackedEventID > 0 {
		v := oldestUnackedEventID
		p.OldestUnackedEventID = &v
	}
	if newestEventID > 0 {
		v := newestEventID
		p.NewestEventID = &v
	}
	if oldestUnackedAcceptedAt != "" {
		v := oldestUnackedAcceptedAt
		p.OldestUnackedAcceptedAt = &v
	}
	js, _ := json.Marshal(p)
	return []byte("event: info\ndata: " + string(js) + "\n\n")
}

// formatBacklogGapEvent builds the backlog_gap control event emitted when the
// replay window has an irrecoverable hole (§2.3, §4.6).
func formatBacklogGapEvent(fromEventID, toEventID int64, reason string) []byte {
	type gapPayload struct {
		Type        string `json:"type"`
		FromEventID int64  `json:"from_event_id"`
		ToEventID   int64  `json:"to_event_id"`
		Reason      string `json:"reason"`
	}
	p := gapPayload{
		Type:        "backlog_gap",
		FromEventID: fromEventID,
		ToEventID:   toEventID,
		Reason:      reason,
	}
	js, _ := json.Marshal(p)
	return []byte("event: backlog_gap\ndata: " + string(js) + "\n\n")
}

// formatHeartbeat returns the SSE comment heartbeat block.
func formatHeartbeat() []byte { return []byte(": heartbeat\n\n") }
