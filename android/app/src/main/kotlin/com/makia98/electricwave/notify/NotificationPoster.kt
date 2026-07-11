package com.makia98.electricwave.notify

import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.content.getSystemService
import com.google.gson.Gson
import com.makia98.electricwave.MainActivity
import com.makia98.electricwave.R
import com.makia98.electricwave.sse.NotificationEvent
import com.makia98.electricwave.util.Logx

/**
 * Posts an SSE notification event as a native Android notification.
 *
 * Mapping rules (per spec):
 *  - priority == "high"  -> [NoticeChannels.URGENT] (requests heads-up)
 *  - normal / low / else -> [NoticeChannels.DEFAULT]
 *  - Server priority never overrides user channel settings.
 *  - Lock-screen visibility is PRIVATE when the user opted to hide sensitive
 *    content; otherwise PUBLIC. The system may still hide content based on the
 *    device lock-screen privacy setting.
 *  - Click cold-starts MainActivity and carries the small `data` payload as a
 *    JSON string extra so the app can route it in-process.
 *  - The Android notification id is a stable hash of the server
 *    `notification_id`, so re-delivery updates the same notification.
 */
object NotificationPoster {

    const val EXTRA_NOTIFICATION_DATA = "notification_data"
    const val EXTRA_NOTIFICATION_ID = "notification_id"

    private const val MAX_TITLE = 80
    private const val MAX_BODY = 500

    fun post(context: Context, event: NotificationEvent, hideSensitiveContent: Boolean) {
        val nm = context.getSystemService<NotificationManager>() ?: return
        val channelId = if (event.priority.equals("high", ignoreCase = true)) {
            NoticeChannels.URGENT
        } else {
            NoticeChannels.DEFAULT
        }

        // If the target channel is disabled by the user, respect that and do not
        // attempt to post (it would be silently dropped anyway). Diagnostics
        // surface this state elsewhere in the UI.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val ch = nm.getNotificationChannel(channelId)
            if (ch != null && ch.importance == NotificationManager.IMPORTANCE_NONE) {
                Logx.w("Channel $channelId disabled by user; notification ${event.notificationId} not shown")
                return
            }
        }

        val title = event.title.take(MAX_TITLE)
        val body = event.body.take(MAX_BODY)

        val dataJson = if (event.data != null) Gson().toJson(event.data) else null
        val tapIntent = Intent(context, MainActivity::class.java).apply {
            action = Intent.ACTION_MAIN
            addCategory(Intent.CATEGORY_LAUNCHER)
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP
            putExtra(EXTRA_NOTIFICATION_ID, event.notificationId)
            if (dataJson != null) putExtra(EXTRA_NOTIFICATION_DATA, dataJson)
        }
        val pendingFlags = PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        val contentIntent = PendingIntent.getActivity(
            context, event.notificationId.hashCode(), tapIntent, pendingFlags
        )

        val visibility = if (hideSensitiveContent) {
            NotificationCompat.VISIBILITY_PRIVATE
        } else {
            NotificationCompat.VISIBILITY_PUBLIC
        }

        val builder = NotificationCompat.Builder(context, channelId)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(NotificationCompat.BigTextStyle().bigText(body))
            .setContentIntent(contentIntent)
            .setAutoCancel(true)
            .setVisibility(visibility)
            .setPriority(
                if (channelId == NoticeChannels.URGENT) {
                    NotificationCompat.PRIORITY_HIGH
                } else {
                    NotificationCompat.PRIORITY_DEFAULT
                }
            )

        if (!event.groupKey.isNullOrBlank()) {
            builder.setGroup(event.groupKey)
        }

        nm.notify(stableId(event.notificationId), builder.build())
    }

    /** Stable 32-bit hash of the server notification id. */
    private fun stableId(key: String): Int {
        var h = 0
        for (i in key.indices) {
            h = 31 * h + key[i].code
        }
        return h
    }
}
