package com.makia98.electricwave.sse

import com.google.gson.annotations.SerializedName

/**
 * SSE `event: notification` payload, matching the integration contract §2.1:
 *
 * ```json
 * {"type":"notification","notification_id":"ntf_...","event_id":42,"title":"...",
 *  "body":"...","priority":"normal","group_key":"...","data":{...},
 *  "expires_at":"<RFC3339 UTC>"}
 * ```
 *
 * [eventId] mirrors the SSE `id:` line value (contract §2.1: the two must be
 * equal). It is injected by [SseClient] from the parsed `id:` line, falling back
 * to the JSON `event_id` field.
 */
data class NotificationEvent(
    @SerializedName("type") val type: String? = null,
    @SerializedName("notification_id") val notificationId: String = "",
    @SerializedName("event_id") val eventId: Long? = null,
    @SerializedName("title") val title: String = "",
    @SerializedName("body") val body: String = "",
    @SerializedName("priority") val priority: String? = "normal",
    @SerializedName("group_key") val groupKey: String? = null,
    /** Arbitrary small business payload; never interpreted as a display command. */
    @SerializedName("data") val data: Map<String, Any?>? = null,
    @SerializedName("expires_at") val expiresAt: String? = null,
)

/**
 * SSE `event: info` control payload (contract §2.2). Sent exactly once after the
 * stream handshake resolves the replay cursor. Does NOT flow through [onEvent];
 * it has its own [SseClient.runOnce] callback.
 */
data class InfoEvent(
    @SerializedName("type") val type: String? = null,
    /** Server's view of the highest event_id this client has acked, or null. */
    @SerializedName("acked_event_id") val ackedEventId: Long? = null,
    @SerializedName("oldest_unacked_event_id") val oldestUnackedEventId: Long? = null,
    @SerializedName("newest_event_id") val newestEventId: Long? = null,
    /** Count of currently queued/sent, non-expired, non-acked events. */
    @SerializedName("backlog_count") val backlogCount: Int = 0,
    /** RFC3339 timestamp of the oldest backlog item ("最老积压时间", §10.4), or null. */
    @SerializedName("oldest_unacked_accepted_at") val oldestUnackedAcceptedAt: String? = null,
)

/**
 * SSE `event: backlog_gap` control payload (contract §2.3). Emitted once after
 * [InfoEvent] when there is an unreplayable hole after the client cursor.
 */
data class BacklogGapEvent(
    @SerializedName("type") val type: String? = null,
    /** Inclusive lower bound of the gap. */
    @SerializedName("from_event_id") val fromEventId: Long = 0,
    /** Inclusive upper bound of the gap. */
    @SerializedName("to_event_id") val toEventId: Long = 0,
    /** One of: retention_exceeded, expired, dropped. */
    @SerializedName("reason") val reason: String? = null,
)
