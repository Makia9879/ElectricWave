package com.makia98.electricwave.service

import android.app.Activity
import android.app.AlarmManager
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import com.makia98.electricwave.util.Logx

/**
 * Asks once a day for the two system grants that actually let a sideloaded
 * receiver survive on HyperOS: battery-optimization exemption, then exact
 * alarms. Autostart itself is a vendor setting and cannot be granted in code.
 */
object SelfStartSupport {

    private const val PREFS = "self_start"
    private const val KEY_PROMPTED = "prompted_at"
    private const val PROMPT_INTERVAL_MS = 24 * 60 * 60 * 1000L

    fun promptIfNeeded(activity: Activity) {
        val prefs = activity.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val now = System.currentTimeMillis()
        if (now - prefs.getLong(KEY_PROMPTED, 0L) < PROMPT_INTERVAL_MS) return

        val pm = activity.getSystemService(PowerManager::class.java)
        if (pm != null && !pm.isIgnoringBatteryOptimizations(activity.packageName)) {
            prefs.edit().putLong(KEY_PROMPTED, now).apply()
            val intent = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS).apply {
                data = Uri.parse("package:${activity.packageName}")
            }
            runCatching { activity.startActivity(intent) }
                .onFailure { Logx.w("Battery exemption prompt failed", it) }
            return
        }

        if (Build.VERSION.SDK_INT >= 31) {
            val am = activity.getSystemService(AlarmManager::class.java)
            if (am != null && !am.canScheduleExactAlarms()) {
                prefs.edit().putLong(KEY_PROMPTED, now).apply()
                val intent = Intent(Settings.ACTION_REQUEST_SCHEDULE_EXACT_ALARM).apply {
                    data = Uri.parse("package:${activity.packageName}")
                }
                runCatching { activity.startActivity(intent) }
                    .onFailure { Logx.w("Exact alarm prompt failed", it) }
            }
        }
    }
}
