package com.makia98.electricwave.sse

import com.google.gson.Gson
import com.makia98.electricwave.data.Profile
import com.makia98.electricwave.util.Logx
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.DisposableHandle
import kotlinx.coroutines.Job
import kotlinx.coroutines.withContext
import kotlin.coroutines.coroutineContext
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import java.io.IOException
import java.net.SocketTimeoutException
import java.net.URLEncoder
import javax.net.ssl.SSLException

/**
 * Minimal, dependency-light Server-Sent Events client over OkHttp.
 *
 * Connect model (per integration contract §3 / spec):
 *  - GET {endpoint}/api/v1/receivers/{receiver_id}/stream
 *  - Headers: Authorization: Bearer <identity_token>, Accept: text/event-stream,
 *    Cache-Control: no-cache, and (per §3) Last-Event-ID + X-Receiver-Ack when
 *    the client has acked at least one event.
 *  - The token is sent ONLY in the Authorization header; never logged, never in
 *    the URL/query.
 *
 * Reliability model: this class handles ONE logical connection attempt and
 * returns why it ended via [Disconnect]. The caller (foreground service) owns
 * the reconnect loop and backoff.
 *
 * Heartbeat watchdog: OkHttp's read timeout is set larger than 2x the server
 * heartbeat interval (server sends a comment every 30s; ~75s of total silence is
 * treated as a dead connection). A read timeout surfaces as [Disconnect.Transient]
 * and triggers a reconnect with backoff.
 */
class SseClient(
    private val client: OkHttpClient,
) {
    /**
     * Why a connection ended. The caller maps each variant to a reconnect or
     * stop decision (contract §10.2).
     */
    sealed interface Disconnect {
        /** Coroutine was cancelled; the caller must stop. */
        object Cancelled : Disconnect
        /** EOF, network error, heartbeat timeout, or 5xx: backoff and retry. */
        object Transient : Disconnect
        /**
         * HTTP 429 or a response carrying `Retry-After`: wait [seconds] (already
         * capped at [RETRY_AFTER_CAP_SECONDS]) then retry.
         */
        data class RetryAfter(val seconds: Long) : Disconnect
        /** HTTP 401/403: stop fast retry, surface auth-failure diagnostics. */
        data class AuthFailure(val httpCode: Int) : Disconnect
        /** HTTP 404: receiver not found; stop retry. */
        data class NotFound(val httpCode: Int = 404) : Disconnect
        /**
         * Endpoint/TLS or other configuration-level error (HTTP 400/405/410/422,
         * SSL handshake failure, or an unbuildable stream URL). Stop retry.
         */
        data class ConfigError(val httpCode: Int = 0, val reason: String = "") : Disconnect
    }

    private val gson = Gson()

    /**
     * The OkHttp [okhttp3.Call] currently blocking on a stream read, if any.
     * The foreground service calls [cancelActive] when superseding a connection
     * so the old blocking `execute()`/`readUtf8Line()` is interrupted promptly —
     * Dispatchers.IO does NOT interrupt blocking IO on coroutine cancellation, so
     * without this the superseded connection would linger and two live streams
     * would ping-pong in the server hub.
     */
    @Volatile
    private var activeCall: okhttp3.Call? = null

    /** Cancels the in-flight stream call, if any. Safe to call from any thread. */
    fun cancelActive() {
        runCatching { activeCall?.cancel() }
    }

    suspend fun runOnce(
        profile: Profile,
        ackedEventId: Long,
        heartbeatTimeoutMs: Long = DEFAULT_HEARTBEAT_TIMEOUT_MS,
        onConnected: () -> Unit,
        onEvent: (NotificationEvent) -> Unit,
        onInfo: (InfoEvent) -> Unit,
        onBacklogGap: (BacklogGapEvent) -> Unit,
        onHeartbeat: () -> Unit,
    ): Disconnect = withContext(Dispatchers.IO) {
        // Pass the current coroutine Job so runOnceBlocking can cancel the OkHttp
        // Call the moment this coroutine is cancelled. Dispatchers.IO does not
        // interrupt blocking IO on cancellation, so without this the superseded
        // connection's execute()/readUtf8Line() would keep blocking and two live
        // streams would ping-pong in the server hub.
        runOnceBlocking(
            profile, ackedEventId, heartbeatTimeoutMs,
            onConnected, onEvent, onInfo, onBacklogGap, onHeartbeat,
            parentJob = coroutineContext[Job],
        )
    }

    @Suppress("ReturnCount")
    private fun runOnceBlocking(
        profile: Profile,
        ackedEventId: Long,
        heartbeatTimeoutMs: Long,
        onConnected: () -> Unit,
        onEvent: (NotificationEvent) -> Unit,
        onInfo: (InfoEvent) -> Unit,
        onBacklogGap: (BacklogGapEvent) -> Unit,
        onHeartbeat: () -> Unit,
        parentJob: Job?,
    ): Disconnect {
        val streamUrl = buildStreamUrl(profile) ?: run {
            Logx.w("Cannot build stream url (invalid endpoint/receiver)")
            return Disconnect.ConfigError(reason = "invalid endpoint or receiver")
        }

        // Token goes ONLY into the header below; never logged.
        val request = Request.Builder()
            .url(streamUrl)
            .header("Authorization", "Bearer ${profile.identityToken}")
            .header("Accept", "text/event-stream")
            .header("Cache-Control", "no-cache")
            .apply {
                // Contract §3: submit Last-Event-ID and X-Receiver-Ack on every
                // connect. Both equal the highest persisted event_id. Omit when 0
                // (nothing acked yet) per §3 ("首次连接为 0 或省略").
                if (ackedEventId > 0L) {
                    header("Last-Event-ID", ackedEventId.toString())
                    header("X-Receiver-Ack", ackedEventId.toString())
                }
            }
            .get()
            .build()

        // Per-call client with a read timeout used as the heartbeat watchdog.
        val callClient = client.newBuilder()
            .readTimeout(heartbeatTimeoutMs, java.util.concurrent.TimeUnit.MILLISECONDS)
            .build()

        var toClose: Response? = null
        val call = callClient.newCall(request)
        activeCall = call
        // Per-job cancellation: cancel THIS Call when the owning coroutine is
        // cancelled. This is the reliable mechanism (each connection owns its own
        // Call); the shared activeCall is only a best-effort backup.
        val cancelHandle: DisposableHandle? = parentJob?.invokeOnCompletion {
            runCatching { call.cancel() }
        }
        try {
            val resp = call.execute()
            toClose = resp
            val code = resp.code
            if (code == 401 || code == 403) {
                Logx.w("Stream rejected: auth failure ($code)")
                return Disconnect.AuthFailure(code)
            }
            if (code == 404) {
                Logx.w("Stream rejected: receiver not found (404)")
                return Disconnect.NotFound(404)
            }
            if (code == 429) {
                val seconds = parseRetryAfter(resp.header("Retry-After"))
                if (seconds != null) {
                    Logx.w("Stream 429 with Retry-After ${seconds}s")
                    return Disconnect.RetryAfter(seconds)
                }
                // 429 without a usable Retry-After: fall back to normal backoff.
                Logx.w("Stream 429 without Retry-After -> transient backoff")
                return Disconnect.Transient
            }
            // Other 4xx (e.g. 400/405/410/422) indicate a configuration problem
            // that will not self-heal by retrying.
            if (code in 400..499) {
                val retryAfter = parseRetryAfter(resp.header("Retry-After"))
                if (retryAfter != null) return Disconnect.RetryAfter(retryAfter)
                Logx.w("Stream config-level rejection: HTTP $code")
                return Disconnect.ConfigError(httpCode = code, reason = "HTTP $code")
            }
            if (code !in 200..299) {
                Logx.w("Stream transient failure: HTTP $code")
                return Disconnect.Transient
            }

            val body = resp.body ?: return Disconnect.Transient
            val source = body.source()
            onConnected()

            val assembler = SseFrameAssembler()

            while (true) {
                val line = source.readUtf8Line()
                if (line == null) {
                    // Clean EOF from the server -> reconnect.
                    Logx.i("Stream EOF")
                    return Disconnect.Transient
                }
                // Heartbeat / comment line: refreshes the idle watchdog.
                if (line.startsWith(":")) {
                    onHeartbeat()
                    continue
                }
                val frame = assembler.consume(line) ?: continue
                dispatchFrame(frame, onEvent, onInfo, onBacklogGap)
            }
        } catch (_: kotlinx.coroutines.CancellationException) {
            return Disconnect.Cancelled
        } catch (_: SocketTimeoutException) {
            // No bytes for heartbeatTimeoutMs -> dead connection.
            Logx.w("Stream read timeout (heartbeat watchdog) -> reconnect")
            return Disconnect.Transient
        } catch (e: SSLException) {
            // TLS / certificate problems are configuration-level: do not retry.
            Logx.w("Stream TLS error -> config error: ${e.javaClass.simpleName}")
            return Disconnect.ConfigError(reason = "tls: ${e.javaClass.simpleName}")
        } catch (e: IOException) {
            Logx.w("Stream IO error -> reconnect: ${e.javaClass.simpleName}")
            return Disconnect.Transient
        } catch (e: Throwable) {
            Logx.e("Stream unexpected error", e)
            return Disconnect.Transient
        } finally {
            cancelHandle?.dispose()
            activeCall = null
            toClose?.close()
        }
    }

    private fun dispatchFrame(
        frame: SseFrame,
        onEvent: (NotificationEvent) -> Unit,
        onInfo: (InfoEvent) -> Unit,
        onBacklogGap: (BacklogGapEvent) -> Unit,
    ) {
        when (frame.event) {
            "notification" -> {
                SsePayloadParser.parseNotification(gson, frame.data, frame.id)?.let(onEvent)
            }
            "info" -> {
                SsePayloadParser.parseInfo(gson, frame.data)?.let(onInfo)
            }
            "backlog_gap" -> {
                SsePayloadParser.parseBacklogGap(gson, frame.data)?.let(onBacklogGap)
            }
            else -> {
                // Unknown event type: ignore (forward-compatible).
            }
        }
    }

    private fun buildStreamUrl(profile: Profile): String? {
        val base = profile.serverEndpoint.trimEnd('/', ' ')
        if (base.isEmpty() || profile.receiverId.isBlank()) return null
        val rid = URLEncoder.encode(profile.receiverId, "UTF-8")
        return "$base/api/v1/receivers/$rid/stream"
    }

    companion object {
        // Server heartbeat is 30s; 2x = 60s; 75s gives margin before declaring
        // the connection dead.
        const val DEFAULT_HEARTBEAT_TIMEOUT_MS = 75_000L

        /** Upper bound for honored Retry-After values (contract §10.2 截断). */
        const val RETRY_AFTER_CAP_SECONDS: Long = 300L

        /**
         * Parses an HTTP `Retry-After` header. Supports the delta-seconds form
         * only (HTTP-date form is intentionally not parsed here). Returns null
         * when absent or unparseable. Result is capped at [RETRY_AFTER_CAP_SECONDS].
         */
        internal fun parseRetryAfter(header: String?): Long? {
            if (header.isNullOrBlank()) return null
            val seconds = header.trim().toLongOrNull() ?: return null
            if (seconds < 0) return null
            return minOf(seconds, RETRY_AFTER_CAP_SECONDS)
        }
    }
}
