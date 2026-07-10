package com.makia98.notice.sse

import com.google.gson.annotations.SerializedName

/**
 * SSE `event: notification` payload, matching the server contract:
 *
 * ```json
 * {"type":"notification","notification_id":"ntf_...","title":"...",
 *  "body":"...","priority":"normal","group_key":"...","data":{...},
 *  "expires_at":"<RFC3339 UTC>"}
 * ```
 */
data class NotificationEvent(
    @SerializedName("type") val type: String? = null,
    @SerializedName("notification_id") val notificationId: String = "",
    @SerializedName("title") val title: String = "",
    @SerializedName("body") val body: String = "",
    @SerializedName("priority") val priority: String? = "normal",
    @SerializedName("group_key") val groupKey: String? = null,
    /** Arbitrary small business payload; never interpreted as a display command. */
    @SerializedName("data") val data: Map<String, Any?>? = null,
    @SerializedName("expires_at") val expiresAt: String? = null,
)
