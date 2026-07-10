package com.makia98.notice

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import com.makia98.notice.notify.NotificationPoster
import com.makia98.notice.ui.NoticeViewModel
import com.makia98.notice.ui.NotificationDetailScreen
import com.makia98.notice.ui.NotificationListScreen
import com.makia98.notice.ui.ProfileScreen
import com.makia98.notice.ui.theme.MakiaTheme

class MainActivity : ComponentActivity() {

    private val viewModel: NoticeViewModel by viewModels()

    // Small payload carried from a notification tap (cold or warm start).
    private var notificationData by mutableStateOf<String?>(null)
    // When opened by tapping a notification, jump straight to its detail.
    private var pendingDetailId by mutableStateOf<String?>(null)
    // DEBUG-only nav request (e.g. adb `am start --es nav list`).
    private var pendingNav by mutableStateOf<String?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        consumeIntent(intent)
        if (BuildConfig.DEBUG) maybeApplyDebugProfile(intent)
        setContent {
            MakiaTheme {
                Surface(modifier = Modifier.fillMaxSize()) {
                    AppNavigation(
                        viewModel = viewModel,
                        notificationData = notificationData,
                        pendingDetailId = pendingDetailId,
                        pendingNav = pendingNav,
                        onConsumeData = { notificationData = null },
                        onConsumePendingDetail = { pendingDetailId = null },
                        onConsumePendingNav = { pendingNav = null },
                    )
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        consumeIntent(intent)
        if (BuildConfig.DEBUG) maybeApplyDebugProfile(intent)
    }

    private fun consumeIntent(intent: Intent?) {
        notificationData = intent?.getStringExtra(NotificationPoster.EXTRA_NOTIFICATION_DATA)
        val id = intent?.getStringExtra(NotificationPoster.EXTRA_NOTIFICATION_ID)
        if (!id.isNullOrBlank()) {
            pendingDetailId = id
            viewModel.markNotificationRead(id)
        }
        if (BuildConfig.DEBUG) {
            intent?.getStringExtra("nav")?.let { pendingNav = it }
        }
    }

    // DEBUG-only: populate the profile from intent extras (adb `am start --es`).
    // Used for automated device tests because HyperOS blocks shell input
    // injection. Release builds skip this entirely (BuildConfig.DEBUG == false)
    // and it never bypasses authentication — the real identity token is required.
    private fun maybeApplyDebugProfile(intent: Intent?) {
        intent ?: return
        val keys = listOf(
            "server_endpoint", "receiver_id", "identity_token",
            "enable", "dev_mode", "hide_sensitive",
        )
        if (keys.none { intent.hasExtra(it) }) return
        val base = viewModel.profile.value
        viewModel.applyDebugProfile(
            endpoint = intent.getStringExtra("server_endpoint") ?: base.serverEndpoint,
            receiverId = intent.getStringExtra("receiver_id") ?: base.receiverId,
            token = intent.getStringExtra("identity_token") ?: base.identityToken,
            devMode = intent.getBooleanExtra("dev_mode", base.devMode),
            hideSensitive = intent.getBooleanExtra("hide_sensitive", base.hideSensitiveContent),
            enable = intent.getBooleanExtra("enable", false),
        )
    }
}

@Composable
private fun AppNavigation(
    viewModel: NoticeViewModel,
    notificationData: String?,
    pendingDetailId: String?,
    pendingNav: String?,
    onConsumeData: () -> Unit,
    onConsumePendingDetail: () -> Unit,
    onConsumePendingNav: () -> Unit,
) {
    var route by rememberSaveable { mutableStateOf("config") }
    var detailId by rememberSaveable { mutableStateOf<String?>(null) }

    // Deep-link: a notification tap opens that notification's detail.
    LaunchedEffect(pendingDetailId) {
        val id = pendingDetailId
        if (!id.isNullOrBlank()) {
            detailId = id
            route = "detail"
            onConsumePendingDetail()
        }
    }

    // DEBUG-only: open a destination directly (adb `am start --es nav list`).
    LaunchedEffect(pendingNav) {
        when (pendingNav) {
            "list" -> { route = "list"; onConsumePendingNav() }
            else -> Unit
        }
    }

    BackHandler(enabled = route != "config") {
        when (route) {
            "detail" -> { route = "list"; detailId = null }
            "list" -> route = "config"
        }
    }

    when (route) {
        "list" -> NotificationListScreen(
            viewModel = viewModel,
            onBack = { route = "config" },
            onOpen = { id ->
                detailId = id
                route = "detail"
                viewModel.markNotificationRead(id)
            },
        )
        "detail" -> NotificationDetailScreen(
            viewModel = viewModel,
            notificationId = detailId,
            onBack = { route = "list"; detailId = null },
        )
        else -> ProfileScreen(
            viewModel = viewModel,
            notificationData = notificationData,
            onConsumeData = onConsumeData,
            onOpenNotifications = { route = "list" },
        )
    }
}
