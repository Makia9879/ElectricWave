package com.makia98.electricwave.service

import android.app.AlarmManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.SystemClock
import com.makia98.electricwave.util.Logx

/**
 * Wakes the process if HyperOS or Android has killed the receiver. The
 * foreground service reschedules this on every start; a user stop cancels it.
 *
 * Exact alarms are used when the user has allowed them. Otherwise an idle-aware
 * inexact alarm is still armed so a granted battery exemption can bring the
 * process back.
 */
object ReceiverScheduler {

    const val ACTION_KEEPALIVE = "com.makia98.electricwave.action.KEEPALIVE"
    private const val REQUEST_CODE = 7101
    const val INTERVAL_MS = 10 * 60 * 1000L
    /** While the screen is off, wake often enough to notice a frozen socket. */
    const val SCREEN_OFF_INTERVAL_MS = 60 * 1000L

    fun schedule(context: Context, delayMs: Long = INTERVAL_MS) {
        val am = context.getSystemService(AlarmManager::class.java) ?: return
        val triggerAt = SystemClock.elapsedRealtime() + delayMs.coerceAtLeast(1_000L)
        val pending = pendingIntent(context)
        try {
            val exactAllowed = Build.VERSION.SDK_INT < 31 || am.canScheduleExactAlarms()
            if (exactAllowed) {
                am.setExactAndAllowWhileIdle(AlarmManager.ELAPSED_REALTIME_WAKEUP, triggerAt, pending)
            } else {
                am.setAndAllowWhileIdle(AlarmManager.ELAPSED_REALTIME_WAKEUP, triggerAt, pending)
            }
        } catch (t: Throwable) {
            Logx.w("Keepalive schedule failed; falling back to inexact", t)
            runCatching {
                am.setAndAllowWhileIdle(AlarmManager.ELAPSED_REALTIME_WAKEUP, triggerAt, pending)
            }
        }
    }

    fun cancel(context: Context) {
        val am = context.getSystemService(AlarmManager::class.java) ?: return
        am.cancel(pendingIntent(context))
    }

    private fun pendingIntent(context: Context): PendingIntent {
        val intent = Intent(context, KeepAliveReceiver::class.java).setAction(ACTION_KEEPALIVE)
        return PendingIntent.getBroadcast(
            context,
            REQUEST_CODE,
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
    }
}
