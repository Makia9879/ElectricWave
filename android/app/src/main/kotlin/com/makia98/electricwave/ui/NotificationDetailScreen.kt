package com.makia98.electricwave.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NotificationDetailScreen(
    viewModel: NoticeViewModel,
    notificationId: String?,
    onBack: () -> Unit,
) {
    val notifications by viewModel.notifications.collectAsStateWithLifecycle()
    val item = notifications.firstOrNull { it.notificationId == notificationId }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("通知详情") },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回")
                    }
                },
            )
        },
    ) { padding ->
        if (item == null) {
            Box(
                modifier = Modifier.fillMaxSize().padding(padding),
                contentAlignment = Alignment.Center,
            ) {
                Text("该通知不存在或已清除", color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        } else {
            val high = item.priority.equals("high", ignoreCase = true)
            Column(
                modifier = Modifier
                    .verticalScroll(rememberScrollState())
                    .padding(16.dp)
                    .padding(padding),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Text(item.title.ifBlank { "(无标题)" }, style = MaterialTheme.typography.headlineSmall)
                DetailRow("优先级", if (high) "high（高优先级）" else item.priority.ifBlank { "normal" })
                item.groupKey?.takeIf { it.isNotBlank() }?.let { DetailRow("分组", it) }
                DetailRow("接收时间", formatDateTime(item.receivedAt))
                item.expiresAt?.takeIf { it.isNotBlank() }?.let { DetailRow("过期时间", it) }
                DetailRow("通知 ID", item.notificationId)
                HorizontalDivider()
                Text("正文", style = MaterialTheme.typography.titleMedium)
                Text(item.body.ifBlank { "(无正文)" }, style = MaterialTheme.typography.bodyLarge)
                if (!item.dataJson.isNullOrBlank()) {
                    Spacer(Modifier.height(8.dp))
                    Text("data 载荷", style = MaterialTheme.typography.titleMedium)
                    Text(
                        text = item.dataJson,
                        style = MaterialTheme.typography.bodyMedium,
                        fontFamily = FontFamily.Monospace,
                    )
                }
            }
        }
    }
}

@Composable
private fun DetailRow(label: String, value: String) {
    Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(label, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(value, modifier = Modifier.padding(start = 16.dp))
    }
}

private fun formatDateTime(ms: Long): String =
    SimpleDateFormat("yyyy-MM-dd HH:mm:ss", Locale.getDefault()).format(Date(ms))
