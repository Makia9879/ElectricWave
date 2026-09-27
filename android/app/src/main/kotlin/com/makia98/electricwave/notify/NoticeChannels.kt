package com.makia98.electricwave.notify

import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.os.Build
import androidx.core.content.getSystemService
import com.makia98.electricwave.R

/**
 * Stable notification channel ids. Once shipped these must never change: users
 * configure per-channel behavior in system settings, keyed by these ids.
 */
object NoticeChannels {
    const val DEFAULT = "default"
    const val URGENT = "urgent"
    const val FOREGROUND = "foreground"

    /** Idempotent: safe to call on every app start. */
    fun create(context: Context) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val nm = context.getSystemService<NotificationManager>() ?: return

        val defaultCh = NotificationChannel(
            DEFAULT,
            context.getString(R.string.channel_default_name),
            NotificationManager.IMPORTANCE_DEFAULT
        ).apply {
            description = context.getString(R.string.channel_default_desc)
            setShowBadge(true)
        }

        val urgentCh = NotificationChannel(
            URGENT,
            context.getString(R.string.channel_urgent_name),
            NotificationManager.IMPORTANCE_HIGH
        ).apply {
            description = context.getString(R.string.channel_urgent_desc)
            setShowBadge(true)
            // Heads-up eligibility is requested via HIGH importance; the system
            // and user settings remain authoritative.
        }

        // Keep this channel quiet. A noisy/default channel made HyperOS treat the
        // receiver as a normal app and drop the SSE connection on screen-off.
        val existing = nm.getNotificationChannel(FOREGROUND)
        if (existing != null && existing.importance != NotificationManager.IMPORTANCE_LOW) {
            nm.deleteNotificationChannel(FOREGROUND)
        }
        val foregroundCh = NotificationChannel(
            FOREGROUND,
            context.getString(R.string.channel_foreground_name),
            NotificationManager.IMPORTANCE_LOW
        ).apply {
            description = context.getString(R.string.channel_foreground_desc)
            setShowBadge(false)
        }

        nm.createNotificationChannels(listOf(defaultCh, urgentCh, foregroundCh))
    }
}
