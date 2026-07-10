package com.makia98.notice.ui

import android.app.Application
import android.app.NotificationManager
import android.content.Context
import android.os.Build
import androidx.core.app.NotificationManagerCompat
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.makia98.notice.NoticeApplication
import com.makia98.notice.data.ConnectionStatus
import com.makia98.notice.data.EndpointValidator
import com.makia98.notice.data.InboxStore
import com.makia98.notice.data.Profile
import com.makia98.notice.data.ProfileStore
import com.makia98.notice.data.ReceivedNotification
import com.makia98.notice.data.RunState
import com.makia98.notice.notify.NoticeChannels
import com.makia98.notice.remote.TestNotifier
import com.makia98.notice.service.NoticeForegroundService
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

class NoticeViewModel(app: Application) : AndroidViewModel(app) {

    private val store: ProfileStore = (app as NoticeApplication).profileStore
    private val inbox: InboxStore = (app as NoticeApplication).inboxStore
    private val tester = TestNotifier((app as NoticeApplication).httpClient)

    val profile: StateFlow<Profile> = store.profile
    val runState: StateFlow<RunState> = store.runState

    /** Received-notification inbox (encrypted at rest). */
    val notifications: StateFlow<List<ReceivedNotification>> = inbox.notifications

    fun markNotificationRead(id: String) = inbox.markRead(id)
    fun clearInbox() = inbox.clear()

    private val _testResult = MutableStateFlow<String?>(null)
    val testResult: StateFlow<String?> = _testResult.asStateFlow()

    private val _endpointError = MutableStateFlow<String?>(null)
    val endpointError: StateFlow<String?> = _endpointError.asStateFlow()

    // ---- Profile edits (persisted; the service picks up changes on restart) ----

    fun updateEndpoint(value: String) {
        val v = value.trim()
        val check = EndpointValidator.validate(v, profile.value.devMode)
        _endpointError.value = if (check.ok) null else check.error
        save(profile.value.copy(serverEndpoint = v))
    }

    fun updateReceiverId(value: String) {
        save(profile.value.copy(receiverId = value.trim()))
    }

    fun updateToken(value: String) {
        // Token is a secret: stored encrypted, never echoed back in logs.
        save(profile.value.copy(identityToken = value))
    }

    fun setDevMode(on: Boolean) {
        val recheck = EndpointValidator.validate(profile.value.serverEndpoint, on)
        _endpointError.value = if (recheck.ok) null else recheck.error
        save(profile.value.copy(devMode = on))
    }

    fun setHideSensitive(on: Boolean) {
        save(profile.value.copy(hideSensitiveContent = on))
    }

    fun setEnabled(on: Boolean) {
        val p = profile.value
        // Refuse to enable an incomplete/invalid profile.
        if (on) {
            val check = EndpointValidator.validate(p.serverEndpoint, p.devMode)
            if (!check.ok || !p.isConnectable) {
                _endpointError.value = check.error ?: "配置不完整"
                save(p.copy(enabled = false))
                return
            }
        }
        val next = p.copy(enabled = on)
        save(next)
        val ctx = getApplication<Application>()
        if (on) {
            NoticeForegroundService.start(ctx)
        } else {
            NoticeForegroundService.stop(ctx)
        }
    }

    fun sendTest() {
        val p = profile.value
        if (!p.isConnectable) {
            _testResult.value = "配置不完整"
            return
        }
        viewModelScope.launch {
            val result = tester.send(p)
            _testResult.value = result
            store.updateRunState { it.copy(lastTestResult = result) }
        }
    }

    fun clearTestResult() {
        _testResult.value = null
    }

    /**
     * DEBUG-only helper to inject a profile via intent extras (adb
     * `am start --es ...`). Avoids depending on UI input injection, which some
     * devices (HyperOS) block for the shell user. Only called from
     * [com.makia98.notice.MainActivity] in debug builds; uses the same encrypted
     * store and foreground-service path as the UI.
     */
    fun applyDebugProfile(
        endpoint: String,
        receiverId: String,
        token: String,
        devMode: Boolean,
        hideSensitive: Boolean,
        enable: Boolean,
    ) {
        val p = Profile(
            serverEndpoint = endpoint.trim(),
            receiverId = receiverId.trim(),
            identityToken = token.trim(),
            devMode = devMode,
            hideSensitiveContent = hideSensitive,
            enabled = false,
        )
        val check = EndpointValidator.validate(p.serverEndpoint, p.devMode)
        _endpointError.value = if (check.ok) null else check.error
        save(p)
        if (enable && check.ok && p.isConnectable) {
            save(p.copy(enabled = true))
            NoticeForegroundService.start(getApplication())
        }
    }

    private fun save(p: Profile) {
        store.save(p)
    }

    // ---- Diagnostics (token-free) ----

    data class Diagnostics(
        val appNotificationsEnabled: Boolean,
        val hasPermission: Boolean,
        val defaultImportance: Int?,
        val urgentImportance: Int?,
        val foregroundImportance: Int?,
    )

    fun snapshotDiagnostics(): Diagnostics {
        val ctx = getApplication<Application>()
        val nm = ctx.getSystemService(Context.NOTIFICATION_SERVICE) as? NotificationManager
        val appOn = NotificationManagerCompat.from(ctx).areNotificationsEnabled()
        val hasPerm = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            ctx.checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) ==
                android.content.pm.PackageManager.PERMISSION_GRANTED
        } else {
            true
        }
        return Diagnostics(
            appNotificationsEnabled = appOn,
            hasPermission = hasPerm,
            defaultImportance = channelImportance(nm, NoticeChannels.DEFAULT),
            urgentImportance = channelImportance(nm, NoticeChannels.URGENT),
            foregroundImportance = channelImportance(nm, NoticeChannels.FOREGROUND),
        )
    }

    private fun channelImportance(nm: NotificationManager?, id: String): Int? {
        if (nm == null || Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return null
        return nm.getNotificationChannel(id)?.importance
    }

    fun statusLabel(status: ConnectionStatus): String = when (status) {
        ConnectionStatus.DISABLED -> "已停用"
        ConnectionStatus.DISCONNECTED -> "已断开"
        ConnectionStatus.CONNECTING -> "连接中"
        ConnectionStatus.CONNECTED -> "已连接"
        ConnectionStatus.BACKOFF -> "退避重连中"
        ConnectionStatus.AUTH_FAILED -> "鉴权失败（停止重试）"
        ConnectionStatus.NOT_FOUND -> "接收端不存在（停止重试）"
    }
}
