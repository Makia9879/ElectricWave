package com.makia98.electricwave.service

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import com.makia98.electricwave.NoticeApplication
import com.makia98.electricwave.util.Logx

/**
 * Best-effort auto-start after boot. This is NOT a guaranteed delivery path:
 * Android 12+ may block background foreground-service starts, and HyperOS
 * auto-start settings must be enabled by the user. Failures are swallowed.
 */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) return
        try {
            val app = context.applicationContext as? NoticeApplication ?: return
            val profile = app.profileStore.current()
            if (profile.enabled && profile.isConnectable) {
                NoticeForegroundService.start(context)
                Logx.i("Boot completed: attempted to start receiving service")
            }
        } catch (t: Throwable) {
            Logx.w("Boot auto-start failed (best-effort)", t)
        }
    }
}
