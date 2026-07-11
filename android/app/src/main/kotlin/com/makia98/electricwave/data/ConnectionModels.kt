package com.makia98.electricwave.data

/**
 * Coarse connection lifecycle state. [isPermanent] flags states that must not
 * trigger fast retry loops (auth failure / unknown receiver / config error) per
 * spec — these transition the UI to [UiStatus.UNAVAILABLE].
 */
enum class ConnectionStatus {
    DISABLED,
    DISCONNECTED,
    CONNECTING,
    CONNECTED,
    BACKOFF,
    AUTH_FAILED,
    NOT_FOUND,
    /** Endpoint/TLS or other non-auth configuration-level error; stop retry. */
    CONFIG_ERROR;

    val isPermanent: Boolean
        get() = this == AUTH_FAILED || this == NOT_FOUND || this == CONFIG_ERROR
}

/**
 * Coarse classification of the last disconnect/error, surfaced on the
 * diagnostics page. Used together with [RunState.status] to drive [UiStatus].
 */
enum class ErrorClass {
    NONE,
    TRANSIENT,
    AUTH,
    NOT_FOUND,
    CONFIG,
    TLS,
    RETRY_AFTER,
    BACKLOG_GAP,
}

/**
 * Persisted runtime snapshot. Written by the foreground service and observed by
 * the UI. Does NOT contain the identity token.
 *
 * Extended for reliable reconnect (contract §10.3): backlog flags, reconnect
 * timing, connection timestamps, last acked event id, and error classification.
 */
data class RunState(
    val status: ConnectionStatus = ConnectionStatus.DISCONNECTED,
    val lastHeartbeatEpochMs: Long? = null,
    val lastError: String? = null,
    val lastTestResult: String? = null,
    val attempt: Int = 0,
    /** True when an `info`/`backlog_gap` control event indicates undelivered backlog. */
    val backlogPending: Boolean = false,
    /** Most recent backlog count reported by the server `info` event. */
    val backlogCount: Int = 0,
    /** Wall-clock ms when the next reconnect attempt is scheduled, or null. */
    val nextReconnectAtMs: Long? = null,
    /** Wall-clock ms of the most recent successful SSE connect, or null. */
    val lastConnectedAtMs: Long? = null,
    /** Wall-clock ms of the most recent disconnect, or null. */
    val lastDisconnectedAtMs: Long? = null,
    /** Highest locally-acked event_id at the time of the snapshot, or null. */
    val lastAckedEventId: Long? = null,
    /** Most recent backlog_gap `to_event_id`, or null if none observed. */
    val backlogGapToEventId: Long? = null,
    /** Wall-clock ms of the oldest backlog item ("最老积压时间", §10.4), or null. */
    val oldestUnackedAcceptedAtMs: Long? = null,
    val errorClass: ErrorClass = ErrorClass.NONE,
)
