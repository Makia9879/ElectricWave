package com.makia98.notice.service

import android.app.Notification
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import com.makia98.notice.R
import com.google.gson.Gson
import com.makia98.notice.data.ConnectionStatus
import com.makia98.notice.data.InboxStore
import com.makia98.notice.data.Profile
import com.makia98.notice.data.ProfileStore
import com.makia98.notice.data.ReceivedNotification
import com.makia98.notice.data.RunState
import com.makia98.notice.notify.NotificationPoster
import com.makia98.notice.notify.NoticeChannels
import com.makia98.notice.sse.SseClient
import com.makia98.notice.util.Logx
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlin.math.min
import kotlin.random.Random

/**
 * Foreground service that owns the SSE lifecycle.
 *
 * - Started explicitly by the user (UI "enable receiving" toggle). Runs with a
 *   low-importance, always-on notification on a dedicated channel.
 * - foregroundServiceType = dataSync (required on API 34+).
 * - Reconnect policy: exponential backoff 1->2->4->8->16->32->60s + jitter, on
 *   EOF, network error, or heartbeat timeout only.
 * - Permanent errors (401/403/404): stop fast retry and surface a diagnosable
 *   state ([ConnectionStatus.AUTH_FAILED] / [ConnectionStatus.NOT_FOUND]).
 * - Disabling the profile locally stops this service and drops the SSE.
 */
class NoticeForegroundService : Service() {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private var streamJob: Job? = null

    private lateinit var store: ProfileStore
    private lateinit var inbox: InboxStore
    private lateinit var sseClient: SseClient

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        store = (application as com.makia98.notice.NoticeApplication).profileStore
        inbox = (application as com.makia98.notice.NoticeApplication).inboxStore
        sseClient = SseClient((application as com.makia98.notice.NoticeApplication).httpClient)
        // Become foreground immediately to satisfy the 5s window.
        startForegroundNotification()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                Logx.i("Stop requested")
                stopStream()
                store.updateRunState { it.copy(status = ConnectionStatus.DISABLED) }
                stopSelf()
                return START_NOT_STICKY
            }
            ACTION_START, null -> {
                val profile = store.current()
                if (!profile.isConnectable) {
                    Logx.w("Start requested but profile not connectable")
                    store.updateRunState {
                        it.copy(status = ConnectionStatus.DISABLED, lastError = "配置不完整")
                    }
                    stopSelf()
                    return START_NOT_STICKY
                }
                startStream(profile)
            }
        }
        // If the process is killed, do not auto-restart the service; the user
        // must re-enable receiving. (Android may still restart a sticky service
        // on some ROMs, which is best-effort.)
        return START_NOT_STICKY
    }

    private fun startStream(profile: Profile) {
        // Cancel any previous loop before starting a fresh one (e.g. reconnect
        // after profile edit).
        streamJob?.cancel()
        store.updateRunState {
            RunState(status = ConnectionStatus.CONNECTING, lastError = null, attempt = 0)
        }
        streamJob = scope.launch {
            var attempt = 0
            while (isActive) {
                val outcome = sseClient.runOnce(
                    profile = profile,
                    onConnected = {
                        attempt = 0
                        store.updateRunState {
                            it.copy(status = ConnectionStatus.CONNECTED, lastError = null)
                        }
                        Logx.i("SSE connected")
                    },
                    onEvent = { event ->
                        // Persist to the encrypted inbox first; bodies may be sensitive.
                        try {
                            inbox.add(
                                ReceivedNotification(
                                    notificationId = event.notificationId,
                                    title = event.title,
                                    body = event.body,
                                    priority = event.priority ?: "normal",
                                    groupKey = event.groupKey,
                                    dataJson = event.data?.let { Gson().toJson(it) },
                                    expiresAt = event.expiresAt,
                                    receivedAt = System.currentTimeMillis(),
                                )
                            )
                        } catch (t: Throwable) {
                            Logx.e("Failed to persist notification ${event.notificationId}", t)
                        }
                        try {
                            NotificationPoster.post(
                                this@NoticeForegroundService,
                                event,
                                profile.hideSensitiveContent,
                            )
                        } catch (t: Throwable) {
                            Logx.e("Failed to post notification ${event.notificationId}", t)
                        }
                    },
                    onHeartbeat = {
                        store.updateRunState {
                            it.copy(lastHeartbeatEpochMs = System.currentTimeMillis())
                        }
                    },
                )
                when (outcome) {
                    SseClient.Disconnect.Cancelled -> break
                    is SseClient.Disconnect.Permanent -> {
                        val status = if (outcome.httpCode == 404) {
                            ConnectionStatus.NOT_FOUND
                        } else {
                            ConnectionStatus.AUTH_FAILED
                        }
                        val label = if (outcome.httpCode == 404) {
                            "接收端不存在 (404)"
                        } else {
                            "鉴权失败 (${outcome.httpCode})"
                        }
                        store.updateRunState {
                            it.copy(status = status, lastError = label)
                        }
                        Logx.w("Permanent stream error -> $label; stopping retry")
                        // Stop the service: profile remains enabled in storage,
                        // but no fast retry. User edits config and re-enables.
                        stopSelfStream()
                        break
                    }
                    SseClient.Disconnect.Transient -> {
                        val waitMs = backoffMs(attempt)
                        attempt += 1
                        store.updateRunState {
                            it.copy(
                                status = ConnectionStatus.BACKOFF,
                                attempt = attempt,
                                lastError = "连接中断，${waitMs / 1000}s 后重连",
                            )
                        }
                        Logx.i("Backing off ${waitMs}ms before reconnect (attempt=$attempt)")
                        delay(waitMs)
                        store.updateRunState {
                            it.copy(status = ConnectionStatus.CONNECTING)
                        }
                    }
                }
            }
        }
    }

    private fun stopSelfStream() {
        streamJob?.cancel()
        stopSelf()
    }

    private fun stopStream() {
        streamJob?.cancel()
        streamJob = null
    }

    override fun onDestroy() {
        stopStream()
        scope.cancel()
        Logx.i("Foreground service destroyed")
        super.onDestroy()
    }

    private fun startForegroundNotification() {
        val notif: Notification = NotificationCompat.Builder(this, NoticeChannels.FOREGROUND)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(getString(R.string.foreground_notif_title))
            .setContentText(getString(R.string.foreground_notif_text))
            .setOngoing(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .build()
        ServiceCompat.startForeground(
            this,
            FOREGROUND_NOTIF_ID,
            notif,
            if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
                ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC
            } else {
                0
            },
        )
    }

    private fun backoffMs(attempt: Int): Long {
        val shift = attempt.coerceAtMost(6)
        val base = 1000L * (1L shl shift) // 1,2,4,8,16,32,64
        val capped = min(base, 60_000L)
        val jitter = Random.nextLong(0, capped / 5 + 1) // up to ~20% jitter
        return capped + jitter
    }

    companion object {
        const val ACTION_START = "com.makia98.notice.action.START"
        const val ACTION_STOP = "com.makia98.notice.action.STOP"
        private const val FOREGROUND_NOTIF_ID = 1001

        fun start(context: Context) {
            val intent = Intent(context, NoticeForegroundService::class.java)
                .setAction(ACTION_START)
            ContextCompat.startForegroundService(context, intent)
        }

        fun stop(context: Context) {
            val intent = Intent(context, NoticeForegroundService::class.java)
                .setAction(ACTION_STOP)
            // Delivered while the app is in the foreground (user toggled off),
            // so a plain startService is allowed.
            try {
                ContextCompat.startForegroundService(context, intent)
            } catch (t: Throwable) {
                Logx.w("stop service intent failed", t)
            }
        }
    }
}
