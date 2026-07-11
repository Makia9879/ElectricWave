package com.makia98.electricwave.service

import android.app.Notification
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.Network
import android.os.IBinder
import androidx.core.app.NotificationCompat
import java.time.Instant
import java.time.format.DateTimeParseException
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner
import com.google.gson.Gson
import com.makia98.electricwave.R
import com.makia98.electricwave.data.AckCursorStore
import com.makia98.electricwave.data.ConnectionStatus
import com.makia98.electricwave.data.ErrorClass
import com.makia98.electricwave.data.InboxStore
import com.makia98.electricwave.data.Profile
import com.makia98.electricwave.data.ProfileStore
import com.makia98.electricwave.data.ReceivedNotification
import com.makia98.electricwave.data.RunState
import com.makia98.electricwave.notify.NotificationPoster
import com.makia98.electricwave.notify.NoticeChannels
import com.makia98.electricwave.sse.BackoffPolicy
import com.makia98.electricwave.sse.SseClient
import com.makia98.electricwave.util.Logx
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/**
 * Foreground service that owns the SSE lifecycle (contract §10.2 / spec 0005).
 *
 * Responsibilities:
 *  - Started explicitly by the user (UI "enable receiving" toggle). Runs with a
 *    low-importance, always-on notification on a dedicated channel.
 *  - foregroundServiceType = dataSync (required on API 34+).
 *  - Maintains a persistent ack cursor ([AckCursorStore]); every connect submits
 *    `Last-Event-ID` / `X-Receiver-Ack`. Notification events are de-duplicated
 *    by event_id, persisted to the encrypted inbox, acked, then posted.
 *  - Reconnect policy: [BackoffPolicy] (1,2,5,10,30,60s, cap 300, ±20% jitter)
 *    on EOF / network error / heartbeat timeout / 5xx. HTTP 429 / `Retry-After`
 *    is honored. Auth/config errors stop auto-reconnect.
 *  - Immediate reconnect (reset backoff) on: user enable, manual reconnect,
 *    App foreground ([ProcessLifecycleOwner]), network recovery
 *    ([ConnectivityManager]), and `onStartCommand`.
 *  - `info` / `backlog_gap` control events update the diagnostics snapshot and
 *    do not produce notifications.
 *
 * Disabling the profile locally stops this service and drops the SSE; the UI
 * then shows the paused state.
 */
class NoticeForegroundService : Service() {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private var streamJob: Job? = null
    private val backoff = BackoffPolicy()
    /**
     * Serializes connection (re)start so that overlapping triggers (foreground /
     * network / onStartCommand / manual) can never launch two stream loops. Java
     * monitors are reentrant, so [triggerReconnect] may call [startStream] while
     * holding this lock.
     */
    private val startLock = Any()

    private lateinit var store: ProfileStore
    private lateinit var inbox: InboxStore
    private lateinit var cursorStore: AckCursorStore
    private lateinit var sseClient: SseClient

    private var networkCallback: ConnectivityManager.NetworkCallback? = null
    private var lifecycleObserver: DefaultLifecycleObserver? = null
    /**
     * registerNetworkCallback delivers onAvailable immediately for the already-up
     * network. That is a registration artifact, not a "network recovered" event
     * (contract §10.2), so the first onAvailable is ignored to avoid a spurious
     * reconnect storm at service start. Reset on unregister for re-registration.
     */
    @Volatile
    private var firstNetworkCallback = true

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        val app = application as com.makia98.electricwave.NoticeApplication
        store = app.profileStore
        inbox = app.inboxStore
        cursorStore = app.ackCursorStore
        sseClient = SseClient(app.httpClient)
        // Become foreground immediately to satisfy the 5s window.
        startForegroundNotification()
        registerLifecycleAndNetwork()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                Logx.i("Stop requested")
                stopStream()
                store.updateRunState {
                    it.copy(
                        status = ConnectionStatus.DISABLED,
                        nextReconnectAtMs = null,
                        errorClass = ErrorClass.NONE,
                    )
                }
                stopSelf()
                return START_NOT_STICKY
            }
            ACTION_RECONNECT -> {
                // Manual one-tap reconnect: reset backoff and try immediately.
                val profile = store.current()
                if (!profile.isConnectable) {
                    Logx.w("Reconnect requested but profile not connectable")
                    return START_NOT_STICKY
                }
                triggerReconnect("manual")
            }
            ACTION_START, null -> {
                val profile = store.current()
                if (!profile.isConnectable) {
                    Logx.w("Start requested but profile not connectable")
                    store.updateRunState {
                        it.copy(
                            status = ConnectionStatus.CONFIG_ERROR,
                            lastError = "配置不完整",
                            errorClass = ErrorClass.CONFIG,
                        )
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
        // Serialize connection (re)starts under startLock. Coroutine cancellation
        // does NOT interrupt the blocking OkHttp call on Dispatchers.IO, so we also
        // call sseClient.cancelActive() to close the superseded Call promptly —
        // otherwise two live streams from overlapping triggers (foreground /
        // network / onStartCommand) ping-pong in the server hub and storm.
        synchronized(startLock) {
        // Cancel any previous loop before starting a fresh one.
        streamJob?.cancel()
        sseClient.cancelActive()
        backoff.reset()
        store.updateRunState {
            it.copy(
                status = ConnectionStatus.CONNECTING,
                lastError = null,
                attempt = 0,
                errorClass = ErrorClass.NONE,
                nextReconnectAtMs = null,
                backlogPending = false,
                backlogCount = 0,
            )
        }
        streamJob = scope.launch {
            while (isActive) {
                val acked = cursorStore.current()
                val outcome = sseClient.runOnce(
                    profile = profile,
                    ackedEventId = acked,
                    onConnected = {
                        store.updateRunState {
                            it.copy(
                                status = ConnectionStatus.CONNECTED,
                                lastError = null,
                                errorClass = ErrorClass.NONE,
                                lastConnectedAtMs = System.currentTimeMillis(),
                                attempt = 0,
                            )
                        }
                        Logx.i("SSE connected (ackedEventId=$acked)")
                    },
                    onEvent = { event ->
                        handleNotificationEvent(event, profile)
                    },
                    onInfo = { info ->
                        val oldestMs = info.oldestUnackedAcceptedAt?.let(::parseEpochMillis)
                        store.updateRunState {
                            it.copy(
                                backlogCount = info.backlogCount,
                                backlogPending = info.backlogCount > 0,
                                lastAckedEventId = info.ackedEventId ?: it.lastAckedEventId,
                                oldestUnackedAcceptedAtMs = oldestMs,
                            )
                        }
                        Logx.i("info: backlog=${info.backlogCount} acked=${info.ackedEventId}")
                    },
                    onBacklogGap = { gap ->
                        store.updateRunState {
                            it.copy(
                                backlogPending = true,
                                backlogGapToEventId = gap.toEventId,
                                errorClass = ErrorClass.BACKLOG_GAP,
                                lastError = "补发缺口 ${gap.fromEventId}..${gap.toEventId} (${gap.reason ?: "unknown"})",
                            )
                        }
                        Logx.w("backlog_gap ${gap.fromEventId}..${gap.toEventId} reason=${gap.reason}")
                    },
                    onHeartbeat = {
                        store.updateRunState {
                            it.copy(lastHeartbeatEpochMs = System.currentTimeMillis())
                        }
                    },
                )
                when (outcome) {
                    SseClient.Disconnect.Cancelled -> break
                    is SseClient.Disconnect.AuthFailure -> {
                        markPermanent(
                            status = ConnectionStatus.AUTH_FAILED,
                            label = "鉴权失败 (${outcome.httpCode})",
                            errorClass = ErrorClass.AUTH,
                        )
                        break
                    }
                    is SseClient.Disconnect.NotFound -> {
                        markPermanent(
                            status = ConnectionStatus.NOT_FOUND,
                            label = "接收端不存在 (404)",
                            errorClass = ErrorClass.NOT_FOUND,
                        )
                        break
                    }
                    is SseClient.Disconnect.ConfigError -> {
                        val ec = if (outcome.reason.startsWith("tls")) ErrorClass.TLS else ErrorClass.CONFIG
                        markPermanent(
                            status = ConnectionStatus.CONFIG_ERROR,
                            label = "配置错误：${outcome.reason}",
                            errorClass = ec,
                        )
                        break
                    }
                    is SseClient.Disconnect.RetryAfter -> {
                        // Honor server-directed delay (already capped at 300s).
                        val waitMs = outcome.seconds * 1000L
                        store.updateRunState {
                            it.copy(
                                status = ConnectionStatus.BACKOFF,
                                errorClass = ErrorClass.RETRY_AFTER,
                                lastError = "服务端限流，${outcome.seconds}s 后重连",
                                lastDisconnectedAtMs = System.currentTimeMillis(),
                                nextReconnectAtMs = System.currentTimeMillis() + waitMs,
                            )
                        }
                        Logx.i("Honoring Retry-After ${outcome.seconds}s")
                        delay(waitMs)
                        store.updateRunState {
                            it.copy(status = ConnectionStatus.CONNECTING, nextReconnectAtMs = null)
                        }
                    }
                    SseClient.Disconnect.Transient -> {
                        val attempt = backoff.currentAttempt()
                        val waitMs = backoff.nextDelayMillis()
                        store.updateRunState {
                            it.copy(
                                status = ConnectionStatus.BACKOFF,
                                attempt = attempt + 1,
                                errorClass = ErrorClass.TRANSIENT,
                                lastError = "连接中断，${waitMs / 1000}s 后重连",
                                lastDisconnectedAtMs = System.currentTimeMillis(),
                                nextReconnectAtMs = System.currentTimeMillis() + waitMs,
                            )
                        }
                        Logx.i("Backing off ${waitMs}ms before reconnect (attempt=${attempt + 1})")
                        delay(waitMs)
                        store.updateRunState {
                            it.copy(status = ConnectionStatus.CONNECTING, nextReconnectAtMs = null)
                        }
                    }
                }
            }
        }
        }
    }

    /**
     * Persist → advance ack cursor → post notification (contract §10.2).
     * Duplicate event_ids (<= acked) are dropped before any side effect.
     */
    private fun handleNotificationEvent(
        event: com.makia98.electricwave.sse.NotificationEvent,
        profile: Profile,
    ) {
        val eid = event.eventId
        if (eid != null && eid <= cursorStore.current()) {
            Logx.i("Duplicate event_id=$eid dropped (acked=${cursorStore.current()})")
            return
        }
        if (eid == null) {
            // Contract requires an id on every notification; defend in depth.
            Logx.w("Notification without event_id accepted without dedup: ${event.notificationId}")
        }
        // 1. Persist to the encrypted inbox first; bodies may be sensitive.
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
                    eventId = eid,
                )
            )
        } catch (t: Throwable) {
            Logx.e("Failed to persist notification ${event.notificationId}", t)
        }
        // 2. Advance the ack cursor and persist (ack does NOT wait for display).
        if (eid != null) {
            cursorStore.advanceTo(eid)
            store.updateRunState { it.copy(lastAckedEventId = cursorStore.current()) }
        }
        // 3. Post the system notification.
        try {
            NotificationPoster.post(this, event, profile.hideSensitiveContent)
        } catch (t: Throwable) {
            Logx.e("Failed to post notification ${event.notificationId}", t)
        }
    }

    private fun markPermanent(
        status: ConnectionStatus,
        label: String,
        errorClass: ErrorClass,
    ) {
        store.updateRunState {
            it.copy(
                status = status,
                lastError = label,
                errorClass = errorClass,
                lastDisconnectedAtMs = System.currentTimeMillis(),
                nextReconnectAtMs = null,
            )
        }
        Logx.w("Permanent stream error -> $label; stopping retry")
        stopSelfStream()
    }

    private fun stopSelfStream() {
        streamJob?.cancel()
        stopSelf()
    }

    private fun stopStream() {
        streamJob?.cancel()
        streamJob = null
    }

    /**
     * Immediate-reconnect guard (contract §10.2). Resets backoff and starts a
     * fresh connection unless the current one is healthy or already connecting.
     */
    private fun triggerReconnect(reason: String) {
        synchronized(startLock) {
            val s = store.currentRunState()
            if (s.status == ConnectionStatus.CONNECTED && isHeartbeatFresh(s)) {
                Logx.i("Reconnect skipped ($reason): connection healthy")
                return
            }
            if (s.status == ConnectionStatus.CONNECTING) {
                Logx.i("Reconnect skipped ($reason): already connecting")
                return
            }
            val profile = store.current()
            if (!profile.enabled || !profile.isConnectable) {
                Logx.w("Reconnect skipped ($reason): profile not connectable")
                return
            }
            Logx.i("Immediate reconnect ($reason)")
            startStream(profile)
        }
    }

    private fun isHeartbeatFresh(state: RunState): Boolean {
        val hb = state.lastHeartbeatEpochMs ?: return false
        return System.currentTimeMillis() - hb < HEARTBEAT_FRESH_MS
    }

    private fun registerLifecycleAndNetwork() {
        // App foreground: ProcessLifecycleOwner ON_START.
        val obs = object : DefaultLifecycleObserver {
            override fun onStart(owner: LifecycleOwner) {
                triggerReconnect("foreground")
            }
        }
        lifecycleObserver = obs
        ProcessLifecycleOwner.get().lifecycle.addObserver(obs)

        // Network recovery: ConnectivityManager default callback.
        val cm = getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
        if (cm != null) {
            val callback = object : ConnectivityManager.NetworkCallback() {
                override fun onAvailable(network: Network) {
                    // Ignore the registration-time delivery for the already-up
                    // network; only act on a genuine recovery (a later onAvailable
                    // after the network was lost).
                    if (firstNetworkCallback) {
                        firstNetworkCallback = false
                        return
                    }
                    triggerReconnect("network")
                }

                override fun onLost(network: Network) {
                    // Mark so the next onAvailable is treated as a real recovery.
                    firstNetworkCallback = false
                }
            }
            networkCallback = callback
            runCatching {
                cm.registerNetworkCallback(
                    android.net.NetworkRequest.Builder()
                        .addCapability(android.net.NetworkCapabilities.NET_CAPABILITY_INTERNET)
                        .build(),
                    callback,
                )
            }.onFailure { Logx.w("Failed to register network callback", it) }
        } else {
            Logx.w("ConnectivityManager unavailable; network-recovery reconnect disabled")
        }
    }

    private fun unregisterLifecycleAndNetwork() {
        lifecycleObserver?.let { ProcessLifecycleOwner.get().lifecycle.removeObserver(it) }
        lifecycleObserver = null
        networkCallback?.let { cb ->
            val cm = getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
            runCatching { cm?.unregisterNetworkCallback(cb) }
        }
        networkCallback = null
        firstNetworkCallback = true
    }

    override fun onDestroy() {
        stopStream()
        unregisterLifecycleAndNetwork()
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

    companion object {
        const val ACTION_START = "com.makia98.electricwave.action.START"
        const val ACTION_STOP = "com.makia98.electricwave.action.STOP"
        const val ACTION_RECONNECT = "com.makia98.electricwave.action.RECONNECT"
        private const val FOREGROUND_NOTIF_ID = 1001
        private const val HEARTBEAT_FRESH_MS = 75_000L

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

        fun reconnect(context: Context) {
            val intent = Intent(context, NoticeForegroundService::class.java)
                .setAction(ACTION_RECONNECT)
            try {
                ContextCompat.startForegroundService(context, intent)
            } catch (t: Throwable) {
                Logx.w("reconnect service intent failed", t)
            }
        }
    }
}

/**
 * Parses an RFC3339 timestamp (e.g. the info event's `oldest_unacked_accepted_at`,
 * §10.4 "最老积压时间") to epoch milliseconds. Returns null on any parse failure
 * so a malformed value never crashes the SSE loop.
 */
private fun parseEpochMillis(rfc3339: String): Long? = try {
    Instant.parse(rfc3339).toEpochMilli()
} catch (_: DateTimeParseException) {
    null
} catch (_: Throwable) {
    null
}
