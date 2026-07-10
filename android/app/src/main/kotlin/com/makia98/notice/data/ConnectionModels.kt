package com.makia98.notice.data

/**
 * Coarse connection lifecycle state. [isPermanent] flags states that must not
 * trigger fast retry loops (auth failure / unknown receiver) per spec.
 */
enum class ConnectionStatus {
    DISABLED,
    DISCONNECTED,
    CONNECTING,
    CONNECTED,
    BACKOFF,
    AUTH_FAILED,
    NOT_FOUND;

    val isPermanent: Boolean
        get() = this == AUTH_FAILED || this == NOT_FOUND
}

/**
 * Persisted runtime snapshot. This is written by the foreground service and
 * observed by the UI. It does NOT contain the identity token.
 */
data class RunState(
    val status: ConnectionStatus = ConnectionStatus.DISCONNECTED,
    val lastHeartbeatEpochMs: Long? = null,
    val lastError: String? = null,
    val lastTestResult: String? = null,
    /** Human-readable, redacted diagnostic (never a token). */
    val attempt: Int = 0,
)
