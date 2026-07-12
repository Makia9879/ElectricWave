package com.makia98.electricwave.data

/**
 * The six user-facing connection states (contract §10.3 / spec 0004). Derived
 * from [RunState] + [DiagnosticFlags] by [UiStatusResolver]. These are the only
 * states shown on the home screen.
 */
enum class UiStatus {
    /** SSE CONNECTED. Main button: none / view details. */
    RECEIVING,
    /** CONNECTING or BACKOFF. Main button: 立即重连. */
    RECONNECTING,
    /** Received backlog_gap or info.backlog_count > 0. Main button: 重连并补发. */
    BACKLOG_PENDING,
    /** Notification permission / channel / foreground notification blocked. */
    NEEDS_AUTHORIZATION,
    /** profile enabled = false. Main button: 开启接收. */
    PAUSED,
    /** AUTH_FAILED / NOT_FOUND / config error. Main button: 检查配置 / 重新绑定. */
    UNAVAILABLE,
}

/**
 * Notification-display capability flags, gathered from Android system APIs by
 * the VM and fed to [UiStatusResolver]. Kept free of Android types so the
 * resolver is unit-testable. "Open" means the user has not disabled it.
 */
data class DiagnosticFlags(
    val appNotificationsEnabled: Boolean = true,
    val postPermissionGranted: Boolean = true,
    val defaultChannelOpen: Boolean = true,
    val urgentChannelOpen: Boolean = true,
    val foregroundChannelOpen: Boolean = true,
)

/**
 * Pure derivation of [UiStatus] from [RunState] + [DiagnosticFlags]
 * (contract §10.3 mapping table). No Android dependency; fully unit-testable.
 *
 * Resolution priority (decided here; the contract table lists conditions but not
 * an explicit ordering — see notes):
 *
 *  1. PAUSED — profile explicitly disabled (overrides everything; the user chose
 *     to stop, so no connection/auth state is authoritative while paused).
 *  2. UNAVAILABLE — permanent config-level failure (AUTH_FAILED / NOT_FOUND /
 *     CONFIG_ERROR). Won't self-heal; user must fix config/rebind.
 *  3. NEEDS_AUTHORIZATION — notification display path blocked by user/system
 *     settings. Surfaced above reception because even a live connection cannot
 *     produce a visible notification in this state.
 *  4. RECONNECTING — an automatic connection attempt is already active
 *     (CONNECTING / BACKOFF); stale backlog metadata must not offer another
 *     "重连并补发" action while that attempt is running.
 *  5. BACKLOG_PENDING — known undelivered backlog while not actively reconnecting.
 *  6. RECEIVING — CONNECTED with nothing pending.
 *  7. RECONNECTING — any remaining disconnected/disabled runtime state.
 */
object UiStatusResolver {

    fun resolve(
        profileEnabled: Boolean,
        runState: RunState,
        flags: DiagnosticFlags,
    ): UiStatus {
        // 1. Paused takes precedence: the user explicitly stopped receiving.
        if (!profileEnabled) return UiStatus.PAUSED

        // 2. Permanent config-level failure.
        if (runState.status.isPermanent) return UiStatus.UNAVAILABLE

        // 3. Notification display authorization.
        if (!needsAuthorization(flags)) {
            // fall through — display path is fine
        } else {
            return UiStatus.NEEDS_AUTHORIZATION
        }

        // 4. An automatic reconnect is already in progress. Keep this above
        // backlog so the UI cannot offer a second reconnect action for stale
        // backlog metadata while CONNECTING/BACKOFF is active.
        if (runState.status == ConnectionStatus.CONNECTING ||
            runState.status == ConnectionStatus.BACKOFF
        ) {
            return UiStatus.RECONNECTING
        }

        // 5. Known backlog pending while no reconnect attempt is active.
        if (runState.backlogPending) return UiStatus.BACKLOG_PENDING

        // 6/7. Reception state.
        return when (runState.status) {
            ConnectionStatus.CONNECTED -> UiStatus.RECEIVING
            ConnectionStatus.CONNECTING,
            ConnectionStatus.BACKOFF,
            ConnectionStatus.DISCONNECTED,
            ConnectionStatus.DISABLED -> UiStatus.RECONNECTING
            // DISABLED here means "no profile" / initial; with profile enabled
            // and not connected, treat as reconnecting so the user has an action.
            ConnectionStatus.AUTH_FAILED,
            ConnectionStatus.NOT_FOUND,
            ConnectionStatus.CONFIG_ERROR -> UiStatus.UNAVAILABLE // already handled above
        }
    }

    /** True iff any notification-display capability is blocked. */
    fun needsAuthorization(flags: DiagnosticFlags): Boolean =
        !flags.appNotificationsEnabled ||
            !flags.postPermissionGranted ||
            !flags.defaultChannelOpen ||
            !flags.urgentChannelOpen ||
            !flags.foregroundChannelOpen
}
