package com.makia98.notice.ui

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
import androidx.compose.material.icons.filled.Notifications
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
import com.makia98.notice.data.ConnectionStatus
import com.makia98.notice.data.Profile
import com.makia98.notice.data.RunState
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
) {
    val profile by viewModel.profile.collectAsStateWithLifecycle()
    val runState by viewModel.runState.collectAsStateWithLifecycle()
    val testResult by viewModel.testResult.collectAsStateWithLifecycle()
    val endpointError by viewModel.endpointError.collectAsStateWithLifecycle()
    val notifications by viewModel.notifications.collectAsStateWithLifecycle()
    val unread = notifications.count { !it.read }

    val context = LocalContext.current
    var showToken by remember { mutableStateOf(false) }
    var diagnostics by remember { mutableStateOf(viewModel.snapshotDiagnostics()) }

    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner) {
        val obs = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) {
                diagnostics = viewModel.snapshotDiagnostics()
            }
        }
        lifecycleOwner.lifecycle.addObserver(obs)
        onDispose { lifecycleOwner.lifecycle.removeObserver(obs) }
    }

    val permLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { diagnostics = viewModel.snapshotDiagnostics() }

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
        diagnostics = viewModel.snapshotDiagnostics()
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Makia通知器") },
                actions = {
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

            ReceivingCard(
                profile = profile,
                runState = runState,
                onToggleEnabled = viewModel::setEnabled,
                statusLabel = viewModel::statusLabel,
            )

            DiagnosticsCard(
                diagnostics = diagnostics,
                onOpenAppSettings = { openAppNotificationSettings(context) },
                onOpenChannelSettings = { id -> openChannelSettings(context, id) },
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
                    "修改配置后请重新开启接收以使新配置生效。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun ReceivingCard(
    profile: Profile,
    runState: RunState,
    onToggleEnabled: (Boolean) -> Unit,
    statusLabel: (ConnectionStatus) -> String,
) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.SpaceBetween,
                modifier = Modifier.fillMaxWidth(),
            ) {
                Column(Modifier.weight(1f)) {
                    Text("接收通知", style = MaterialTheme.typography.titleMedium)
                    Text(
                        "开启后运行前台服务并保持 SSE 连接",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Switch(checked = profile.enabled, onCheckedChange = onToggleEnabled)
            }

            HorizontalDivider()

            StatusRow(label = "连接状态", value = statusLabel(runState.status))
            StatusRow(label = "最后心跳", value = formatTime(runState.lastHeartbeatEpochMs))
            StatusRow(label = "重试次数", value = runState.attempt.toString())
            runState.lastError?.let {
                StatusRow(label = "最后错误", value = it)
            }
            runState.lastTestResult?.let {
                StatusRow(label = "上次测试", value = it)
            }
        }
    }
}

@Composable
private fun DiagnosticsCard(
    diagnostics: NoticeViewModel.Diagnostics,
    onOpenAppSettings: () -> Unit,
    onOpenChannelSettings: (String) -> Unit,
) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("通知权限与渠道诊断", style = MaterialTheme.typography.titleMedium)

            StatusRow(
                label = "应用通知总开关",
                value = if (diagnostics.appNotificationsEnabled) "已开启" else "已关闭",
            )
            StatusRow(
                label = "通知权限",
                value = if (diagnostics.hasPermission) "已授予" else "未授予",
            )
            ChannelRow(
                name = "default 渠道",
                importance = diagnostics.defaultImportance,
                onOpen = { onOpenChannelSettings("default") },
            )
            ChannelRow(
                name = "urgent 渠道",
                importance = diagnostics.urgentImportance,
                onOpen = { onOpenChannelSettings("urgent") },
            )
            ChannelRow(
                name = "foreground 渠道",
                importance = diagnostics.foregroundImportance,
                onOpen = { onOpenChannelSettings("foreground") },
            )

            if (!diagnostics.appNotificationsEnabled || !diagnostics.hasPermission) {
                Text(
                    "通知权限或渠道被关闭时，App 无法显示业务通知。请在系统设置中开启。App 不会尝试绕过系统设置。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                )
            }

            OutlinedButton(onClick = onOpenAppSettings, modifier = Modifier.fillMaxWidth()) {
                Text("打开应用通知设置")
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

@Composable
private fun ChannelRow(name: String, importance: Int?, onOpen: () -> Unit) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Text(name, style = MaterialTheme.typography.bodyMedium)
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(importanceLabel(importance), style = MaterialTheme.typography.bodyMedium)
            TextButton(onClick = onOpen) { Text("设置") }
        }
    }
}

private fun importanceLabel(importance: Int?): String = when (importance) {
    null -> "未知"
    NotificationManager.IMPORTANCE_NONE -> "已关闭"
    NotificationManager.IMPORTANCE_MIN -> "最低"
    NotificationManager.IMPORTANCE_LOW -> "低"
    NotificationManager.IMPORTANCE_DEFAULT -> "默认"
    NotificationManager.IMPORTANCE_HIGH -> "高（可弹出）"
    else -> importance.toString()
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

private fun openChannelSettings(context: android.content.Context, channelId: String) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
    val intent = Intent(Settings.ACTION_CHANNEL_NOTIFICATION_SETTINGS).apply {
        putExtra(EXTRA_APP_PACKAGE, context.packageName)
        putExtra(Settings.EXTRA_CHANNEL_ID, channelId)
        addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    }
    runCatching { context.startActivity(intent) }
}
