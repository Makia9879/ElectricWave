package com.makia98.electricwave.service

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import com.makia98.electricwave.NoticeApplication
import com.makia98.electricwave.util.Logx

/**
 * Alarm entry point. Starts the receiving service if the saved profile is
 * enabled, then arms the next wake. Does nothing (and cancels the alarm) when
 * the user has turned receiving off.
 */
class KeepAliveReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent?) {
        if (intent?.action != ReceiverScheduler.ACTION_KEEPALIVE) return
        try {
            val app = context.applicationContext as? NoticeApplication ?: return
            val profile = app.profileStore.current()
            if (!profile.enabled || !profile.isConnectable) {
                ReceiverScheduler.cancel(context)
                return
            }
            Logx.i("Keepalive: watchdog")
            NoticeForegroundService.watchdog(context)
        } catch (t: Throwable) {
            Logx.w("Keepalive failed", t)
        }
    }
}
