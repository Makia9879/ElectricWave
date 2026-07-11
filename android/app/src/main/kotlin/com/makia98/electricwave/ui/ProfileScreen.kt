package com.makia98.electricwave.ui

import android.app.NotificationManager
import android.content.Intent
import android.os.Build
import android.provider.Settings
import android.provider.Settings.ACTION_APP_NOTIFICATION_SETTINGS
import android.provider.Settings.EXTRA_APP_PACKAGE
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Visibility
import androidx.compose.material.icons.filled.VisibilityOff
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.makia98.electricwave.data.Profile
import com.makia98.electricwave.data.RunState
import com.makia98.electricwave.data.UiStatus
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ProfileScreen(
    viewModel: NoticeViewModel,
    notificationData: String?,
    onConsumeData: () -> Unit,
    onOpenNotifications: () -> Unit,
    onOpenDiagnostics: () -> Unit,
) {
    val profile by viewModel.profile.collectAsStateWithLifecycle()
    val runState by viewModel.runState.collectAsStateWithLifecycle()
    val uiStatus by viewModel.uiStatus.collectAsStateWithLifecycle()
    val testResult by viewModel.testResult.collectAsStateWithLifecycle()
    val endpointError by viewModel.endpointError.collectAsStateWithLifecycle()
    val diagnostics by viewModel.diagnostics.collectAsStateWithLifecycle()
    val notifications by viewModel.notifications.collectAsStateWithLifecycle()
    val unread = notifications.count { !it.read }

    val context = LocalContext.current
    var showToken by remember { mutableStateOf(false) }

    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner) {
        val obs = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) {
                viewModel.refreshDiagnostics()
            }
        }
        lifecycleOwner.lifecycle.addObserver(obs)
        onDispose { lifecycleOwner.lifecycle.removeObserver(obs) }
    }

    val permLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { viewModel.refreshDiagnostics() }

    LaunchedEffect(Unit) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            val granted = ContextCompat.checkSelfPermission(
                context,
                android.Manifest.permission.POST_NOTIFICATIONS,
            ) == android.content.pm.PackageManager.PERMISSION_GRANTED
            if (!granted) {
                permLauncher.launch(android.Manifest.permission.POST_NOTIFICATIONS)
            }
        }
        viewModel.refreshDiagnostics()
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("ElectricWave") },
                actions = {
                    IconButton(onClick = onOpenDiagnostics) {
                        Icon(
                            imageVector = Icons.Filled.Info,
                            contentDescription = "诊断详情",
                        )
                    }
                    BadgedBox(
                        badge = { if (unread > 0) Badge { Text(unread.toString()) } },
                    ) {
                        IconButton(onClick = onOpenNotifications) {
                            Icon(
                                imageVector = Icons.Filled.Notifications,
                                contentDescription = "通知列表",
                            )
                        }
                    }
                },
            )
        }
    ) { padding ->
        Column(
            modifier = Modifier
                .verticalScroll(rememberScrollState())
                .padding(16.dp)
                .padding(padding),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            StatusCard(
                uiStatus = uiStatus,
                runState = runState,
                enabled = profile.enabled,
                onToggleEnabled = viewModel::setEnabled,
                onPrimary = { primaryAction(uiStatus, viewModel, context) },
                primaryLabel = primaryButtonLabel(uiStatus),
                onOpenDiagnostics = onOpenDiagnostics,
            )

            ConfigCard(
                profile = profile,
                endpointError = endpointError,
                showToken = showToken,
                onToggleShowToken = { showToken = !showToken },
                onEndpoint = viewModel::updateEndpoint,
                onReceiverId = viewModel::updateReceiverId,
                onToken = viewModel::updateToken,
                onDevMode = viewModel::setDevMode,
                onHideSensitive = viewModel::setHideSensitive,
            )

            TestCard(
                connectable = profile.isConnectable,
                testResult = testResult,
                onSend = viewModel::sendTest,
                onClear = viewModel::clearTestResult,
            )

            notificationData?.let { data ->
                DataPayloadCard(data = data, onDismiss = onConsumeData)
            }

            Spacer(Modifier.height(24.dp))
        }
    }
}

private fun primaryButtonLabel(status: UiStatus): String? = when (status) {
    UiStatus.RECEIVING -> null
    UiStatus.RECONNECTING -> "立即重连"
    UiStatus.BACKLOG_PENDING -> "重连并补发"
    UiStatus.NEEDS_AUTHORIZATION -> "去授权/设置"
    UiStatus.PAUSED -> "开启接收"
    UiStatus.UNAVAILABLE -> "检查配置后重连"
}

private fun primaryAction(
    status: UiStatus,
    viewModel: NoticeViewModel,
    context: android.content.Context,
) {
    when (status) {
        UiStatus.RECEIVING -> Unit
        UiStatus.RECONNECTING,
        UiStatus.BACKLOG_PENDING,
        UiStatus.UNAVAILABLE -> viewModel.reconnectNow()
        UiStatus.NEEDS_AUTHORIZATION -> openAppNotificationSettings(context)
        UiStatus.PAUSED -> viewModel.setEnabled(true)
    }
}

@Composable
private fun StatusCard(
    uiStatus: UiStatus,
    runState: RunState,
    enabled: Boolean,
    onToggleEnabled: (Boolean) -> Unit,
    onPrimary: () -> Unit,
    primaryLabel: String?,
    onOpenDiagnostics: () -> Unit,
) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.SpaceBetween,
                modifier = Modifier.fillMaxWidth(),
            ) {
                Column(Modifier.weight(1f)) {
                    Text("连接状态", style = MaterialTheme.typography.titleMedium)
                    Text(
                        text = uiStatusLabel(uiStatus),
                        style = MaterialTheme.typography.headlineSmall,
                        color = uiStatusColor(uiStatus),
                    )
                }
                Switch(checked = enabled, onCheckedChange = onToggleEnabled)
            }
            uiStatusHint(uiStatus)?.let {
                Text(
                    text = it,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            HorizontalDivider()
            StatusRow(label = "SSE", value = viewModelStatusLabel(runState))
            StatusRow(label = "最后心跳", value = formatTime(runState.lastHeartbeatEpochMs))
            if (runState.backlogPending) {
                StatusRow(label = "待补发", value = "${runState.backlogCount} 条")
            }
            runState.lastAckedEventId?.let {
                StatusRow(label = "最近 ack event_id", value = it.toString())
            }
            StatusRow(label = "重试次数", value = runState.attempt.toString())
            runState.lastError?.let {
                StatusRow(label = "最后错误", value = it)
            }

            primaryLabel?.let {
                Spacer(Modifier.height(4.dp))
                Button(onClick = onPrimary, modifier = Modifier.fillMaxWidth()) {
                    Icon(Icons.Filled.Refresh, contentDescription = null)
                    Spacer(Modifier.height(0.dp))
                    Text("  $it")
                }
            }
            OutlinedButton(onClick = onOpenDiagnostics, modifier = Modifier.fillMaxWidth()) {
                Text("查看诊断详情")
            }
        }
    }
}

@Composable
private fun uiStatusColor(status: UiStatus) = when (status) {
    UiStatus.RECEIVING -> MaterialTheme.colorScheme.primary
    UiStatus.RECONNECTING -> MaterialTheme.colorScheme.tertiary
    UiStatus.BACKLOG_PENDING -> MaterialTheme.colorScheme.secondary
    UiStatus.NEEDS_AUTHORIZATION -> MaterialTheme.colorScheme.error
    UiStatus.PAUSED -> MaterialTheme.colorScheme.onSurfaceVariant
    UiStatus.UNAVAILABLE -> MaterialTheme.colorScheme.error
}

private fun uiStatusLabel(status: UiStatus): String = when (status) {
    UiStatus.RECEIVING -> "接收中"
    UiStatus.RECONNECTING -> "正在重连"
    UiStatus.BACKLOG_PENDING -> "有待补发"
    UiStatus.NEEDS_AUTHORIZATION -> "需要授权"
    UiStatus.PAUSED -> "已暂停"
    UiStatus.UNAVAILABLE -> "不可用"
}

private fun uiStatusHint(status: UiStatus): String? = when (status) {
    UiStatus.RECEIVING -> "SSE 已连接，可接收事件。不代表通知一定展示在用户面前。"
    UiStatus.RECONNECTING -> "连接中断或正在建立，按指数退避自动重连。"
    UiStatus.BACKLOG_PENDING -> "服务端有未投递的积压消息，重连后将按 event_id 补发。"
    UiStatus.NEEDS_AUTHORIZATION -> "通知权限或渠道被关闭，App 无法保证通知展示。"
    UiStatus.PAUSED -> "接收已暂停，不会自动重连。"
    UiStatus.UNAVAILABLE -> "配置级错误（鉴权/接收端/TLS/endpoint），不会自动重连。"
}

private fun viewModelStatusLabel(state: RunState): String = when (state.status) {
    com.makia98.electricwave.data.ConnectionStatus.DISABLED -> "已停用"
    com.makia98.electricwave.data.ConnectionStatus.DISCONNECTED -> "已断开"
    com.makia98.electricwave.data.ConnectionStatus.CONNECTING -> "连接中"
    com.makia98.electricwave.data.ConnectionStatus.CONNECTED -> "已连接"
    com.makia98.electricwave.data.ConnectionStatus.BACKOFF -> "退避中"
    com.makia98.electricwave.data.ConnectionStatus.AUTH_FAILED -> "鉴权失败"
    com.makia98.electricwave.data.ConnectionStatus.NOT_FOUND -> "接收端不存在"
    com.makia98.electricwave.data.ConnectionStatus.CONFIG_ERROR -> "配置错误"
}

@Composable
private fun ConfigCard(
    profile: Profile,
    endpointError: String?,
    showToken: Boolean,
    onToggleShowToken: () -> Unit,
    onEndpoint: (String) -> Unit,
    onReceiverId: (String) -> Unit,
    onToken: (String) -> Unit,
    onDevMode: (Boolean) -> Unit,
    onHideSensitive: (Boolean) -> Unit,
) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("接收端配置", style = MaterialTheme.typography.titleMedium)

            OutlinedTextField(
                value = profile.serverEndpoint,
                onValueChange = onEndpoint,
                label = { Text("服务器地址 (https://...)") },
                singleLine = true,
                isError = endpointError != null,
                supportingText = { endpointError?.let { Text(it) } },
                modifier = Modifier.fillMaxWidth(),
            )

            OutlinedTextField(
                value = profile.receiverId,
                onValueChange = onReceiverId,
                label = { Text("接收端 ID (receiver_id)") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )

            // Identity token: masked by default; secret, never logged anywhere.
            OutlinedTextField(
                value = profile.identityToken,
                onValueChange = onToken,
                label = { Text("接收端身份令牌") },
                singleLine = true,
                visualTransformation = if (showToken) {
                    VisualTransformation.None
                } else {
                    PasswordVisualTransformation()
                },
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                trailingIcon = {
                    IconButton(onClick = onToggleShowToken) {
                        Icon(
                            imageVector = if (showToken) Icons.Filled.VisibilityOff else Icons.Filled.Visibility,
                            contentDescription = if (showToken) "隐藏令牌" else "显示令牌",
                        )
                    }
                },
                modifier = Modifier.fillMaxWidth(),
            )

            HorizontalDivider()

            SwitchRow(
                title = "开发模式",
                subtitle = "允许 http:// 本地/内网地址（默认关闭）",
                checked = profile.devMode,
                onChange = onDevMode,
            )
            SwitchRow(
                title = "锁屏隐藏敏感内容",
                subtitle = "开启后通知在锁屏使用私密可见性",
                checked = profile.hideSensitiveContent,
                onChange = onHideSensitive,
            )

            if (profile.enabled) {
                Text(
                    "修改配置后请关闭再重新开启接收以使新配置生效。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun TestCard(
    connectable: Boolean,
    testResult: String?,
    onSend: () -> Unit,
    onClear: () -> Unit,
) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("链路测试", style = MaterialTheme.typography.titleMedium)
            Text(
                "向服务端请求发送一条测试通知（POST .../test）。",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Button(
                onClick = onSend,
                enabled = connectable,
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text("发送测试通知")
            }
            testResult?.let {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.SpaceBetween,
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Text("结果：$it", modifier = Modifier.weight(1f))
                    TextButton(onClick = onClear) { Text("清除") }
                }
            }
        }
    }
}

@Composable
private fun DataPayloadCard(data: String, onDismiss: () -> Unit) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.primaryContainer,
        ),
    ) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("来自通知点击的数据载荷", style = MaterialTheme.typography.titleSmall)
            Text(
                text = data,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
            )
            TextButton(onClick = onDismiss) { Text("关闭") }
        }
    }
}

@Composable
private fun SwitchRow(title: String, subtitle: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Column(Modifier.weight(1f)) {
            Text(title)
            Text(
                subtitle,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Switch(checked = checked, onCheckedChange = onChange)
    }
}

@Composable
private fun StatusRow(label: String, value: String) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Text(label, style = MaterialTheme.typography.bodyMedium)
        Text(value, style = MaterialTheme.typography.bodyMedium)
    }
}

private fun formatTime(ms: Long?): String {
    if (ms == null) return "—"
    return SimpleDateFormat("HH:mm:ss", Locale.getDefault()).format(Date(ms))
}

private fun openAppNotificationSettings(context: android.content.Context) {
    val intent = Intent(ACTION_APP_NOTIFICATION_SETTINGS).apply {
        putExtra(EXTRA_APP_PACKAGE, context.packageName)
        addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    }
    runCatching { context.startActivity(intent) }
}
