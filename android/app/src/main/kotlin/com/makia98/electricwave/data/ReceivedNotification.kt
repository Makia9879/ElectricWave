package com.makia98.electricwave.data

/**
 * A notification received over SSE and persisted in the encrypted inbox so the
 * user can browse history and open details. Bodies may be sensitive, so the
 * inbox is stored in EncryptedSharedPreferences (Keystore-backed).
 *
 * SECURITY: like [Profile.identityToken], [body]/[dataJson] are secrets; this
 * type's `toString()` is overridden to redact them so a stray log can't leak
 * notification content.
 */
data class ReceivedNotification(
    val notificationId: String,
    val title: String,
    val body: String,
    val priority: String,
    val groupKey: String?,
    val dataJson: String?,
    val expiresAt: String?,
    val receivedAt: Long,
    val read: Boolean = false,
    /** Server-assigned monotonic event_id (contract §1). 0/null on legacy rows. */
    val eventId: Long? = null,
) {
    override fun toString(): String =
        "ReceivedNotification(id=$notificationId, eventId=$eventId, title=$title, " +
            "priority=$priority, read=$read, receivedAt=$receivedAt, " +
            "body=<redacted>, data=<redacted>)"
}
