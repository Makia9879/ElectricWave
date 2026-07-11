package com.makia98.electricwave.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.makia98.electricwave.data.ConnectionStatus
import com.makia98.electricwave.data.ErrorClass
import com.makia98.electricwave.data.RunState
import com.makia98.electricwave.data.UiStatus
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/**
 * Diagnostics detail page (contract §10.4). Read-only snapshot of the SSE state,
 * timing, backlog, notification-display capability, and OEM/system reliability
 * conditions. Honest about what the app cannot read programmatically: OEM
 * autostart / background / battery settings are flagged "需要用户确认" rather
 * than claimed as guaranteed.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DiagnosticsScreen(
    viewModel: NoticeViewModel,
    onBack: () -> Unit,
    onOpenAppNotificationSettings: () -> Unit,
    onOpenChannelSettings: (String) -> Unit,
) {
    val profile by viewModel.profile.collectAsStateWithLifecycle()
    val runState by viewModel.runState.collectAsStateWithLifecycle()
    val uiStatus by viewModel.uiStatus.collectAsStateWithLifecycle()
    val diagnostics by viewModel.diagnostics.collectAsStateWithLifecycle()

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("诊断详情") },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回")
                    }
                },
            )
        },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(16.dp)
                .padding(padding),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            SectionCard("概览") {
                DetailRow("UI 状态", uiStatusLabel(uiStatus))
                DetailRow("SSE", connectionLabel(runState.status))
                DetailRow("profile enabled", profile.enabled.toString())
                DetailRow("receiver_id", profile.receiverId.ifBlank { "—" })
                DetailRow("endpoint", profile.serverEndpoint.ifBlank { "—" })
                DetailRow("令牌", if (profile.tokenPresent) "已设置（不显示）" else "未设置")
            }

            SectionCard("SSE 时序") {
                DetailRow("最近心跳", formatTime(runState.lastHeartbeatEpochMs))
                DetailRow("最近连接", formatTime(runState.lastConnectedAtMs))
                DetailRow("最近断开", formatTime(runState.lastDisconnectedAtMs))
                DetailRow("下次重连", formatTime(runState.nextReconnectAtMs))
                DetailRow("重试次数", runState.attempt.toString())
            }

            SectionCard("积压与补发") {
                DetailRow("待补发", if (runState.backlogPending) "是" else "否")
                DetailRow("backlog 数量", runState.backlogCount.toString())
                DetailRow("最老积压时间", formatTime(runState.oldestUnackedAcceptedAtMs))
                runState.backlogGapToEventId?.let {
                    DetailRow("最近缺口至 event_id", it.toString())
                }
                DetailRow("最近 ack event_id", runState.lastAckedEventId?.toString() ?: "—")
            }

            SectionCard("最近错误") {
                DetailRow("错误分类", errorClassLabel(runState.errorClass))
                DetailRow("描述", runState.lastError ?: "—")
                Text(
                    nextStepHint(uiStatus, runState),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            SectionCard("通知展示能力") {
                DetailRow(
                    "应用通知总开关",
                    if (diagnostics.appNotificationsEnabled) "已开启" else "已关闭",
                )
                DetailRow("通知权限", if (diagnostics.hasPermission) "已授予" else "未授予")
                ChannelDetailRow(
                    name = "default 渠道",
                    importance = diagnostics.defaultImportance,
                    onOpen = { onOpenChannelSettings("default") },
                )
                ChannelDetailRow(
                    name = "urgent 渠道",
                    importance = diagnostics.urgentImportance,
                    onOpen = { onOpenChannelSettings("urgent") },
                )
                ChannelDetailRow(
                    name = "foreground 渠道",
                    importance = diagnostics.foregroundImportance,
                    onOpen = { onOpenChannelSettings("foreground") },
                )
                if (!diagnostics.appNotificationsEnabled || !diagnostics.hasPermission) {
                    Text(
                        "通知权限或渠道被关闭时，App 无法显示业务通知。App 不会尝试绕过系统设置。",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
                OutlinedButton(onClick = onOpenAppNotificationSettings, modifier = Modifier.fillMaxWidth()) {
                    Text("打开应用通知设置")
                }
            }

            SectionCard("系统可靠性条件") {
                DetailRow("开机自启动", "需要用户确认")
                DetailRow("后台运行", "需要用户确认")
                DetailRow("电池/省电策略", "需要用户确认")
                Text(
                    "Android 标准无法稳定读取 HyperOS/MIUI 的自启动、后台运行与省电开关。" +
                        "请在系统设置中确认已放行本 App；未放行时不承诺熄屏或开机后自动恢复。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun SectionCard(title: String, content: @Composable () -> Unit) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(title, style = MaterialTheme.typography.titleMedium)
            content()
        }
    }
}

@Composable
private fun DetailRow(label: String, value: String) {
    Row(
        verticalAlignment = androidx.compose.ui.Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Text(label, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(value, style = MaterialTheme.typography.bodyMedium)
    }
}

@Composable
private fun ChannelDetailRow(name: String, importance: Int?, onOpen: () -> Unit) {
    Row(
        verticalAlignment = androidx.compose.ui.Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Text(name, style = MaterialTheme.typography.bodyMedium)
        Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
            Text(importanceLabel(importance), style = MaterialTheme.typography.bodyMedium)
            TextButton(onClick = onOpen) { Text("设置") }
        }
    }
}

private fun uiStatusLabel(status: UiStatus): String = when (status) {
    UiStatus.RECEIVING -> "接收中"
    UiStatus.RECONNECTING -> "正在重连"
    UiStatus.BACKLOG_PENDING -> "有待补发"
    UiStatus.NEEDS_AUTHORIZATION -> "需要授权"
    UiStatus.PAUSED -> "已暂停"
    UiStatus.UNAVAILABLE -> "不可用"
}

private fun connectionLabel(status: ConnectionStatus): String = when (status) {
    ConnectionStatus.DISABLED -> "已停用"
    ConnectionStatus.DISCONNECTED -> "已断开"
    ConnectionStatus.CONNECTING -> "连接中"
    ConnectionStatus.CONNECTED -> "已连接"
    ConnectionStatus.BACKOFF -> "退避重连中"
    ConnectionStatus.AUTH_FAILED -> "鉴权失败"
    ConnectionStatus.NOT_FOUND -> "接收端不存在"
    ConnectionStatus.CONFIG_ERROR -> "配置错误"
}

private fun errorClassLabel(ec: ErrorClass): String = when (ec) {
    ErrorClass.NONE -> "无"
    ErrorClass.TRANSIENT -> "瞬时（EOF/网络/5xx/心跳超时）"
    ErrorClass.AUTH -> "鉴权失败 (401/403)"
    ErrorClass.NOT_FOUND -> "接收端不存在 (404)"
    ErrorClass.CONFIG -> "配置错误"
    ErrorClass.TLS -> "TLS/证书错误"
    ErrorClass.RETRY_AFTER -> "服务端限流 (Retry-After)"
    ErrorClass.BACKLOG_GAP -> "补发缺口（非错误）"
}

private fun nextStepHint(status: UiStatus, runState: RunState): String = when (status) {
    UiStatus.RECEIVING -> "连接正常，无需操作。"
    UiStatus.RECONNECTING -> "点击\"立即重连\"重置退避并立即尝试。"
    UiStatus.BACKLOG_PENDING -> "点击\"重连并补发\"建立连接并按 event_id 补发积压消息。"
    UiStatus.NEEDS_AUTHORIZATION -> "请在系统设置中开启通知权限与相关渠道。"
    UiStatus.PAUSED -> "点击\"开启接收\"恢复。"
    UiStatus.UNAVAILABLE -> "检查 endpoint/receiver_id/令牌是否正确，或重新绑定后重连。"
}

private fun importanceLabel(importance: Int?): String = when (importance) {
    null -> "未知"
    android.app.NotificationManager.IMPORTANCE_NONE -> "已关闭"
    android.app.NotificationManager.IMPORTANCE_MIN -> "最低"
    android.app.NotificationManager.IMPORTANCE_LOW -> "低"
    android.app.NotificationManager.IMPORTANCE_DEFAULT -> "默认"
    android.app.NotificationManager.IMPORTANCE_HIGH -> "高（可弹出）"
    else -> importance.toString()
}

private fun formatTime(ms: Long?): String {
    if (ms == null) return "—"
    return SimpleDateFormat("yyyy-MM-dd HH:mm:ss", Locale.getDefault()).format(Date(ms))
}
