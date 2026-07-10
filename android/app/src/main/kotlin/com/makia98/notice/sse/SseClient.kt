package com.makia98.notice.sse

import com.google.gson.Gson
import com.google.gson.JsonSyntaxException
import com.makia98.notice.data.Profile
import com.makia98.notice.util.Logx
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import java.io.IOException
import java.net.SocketTimeoutException
import java.net.URLEncoder

/**
 * Minimal, dependency-light Server-Sent Events client over OkHttp.
 *
 * Connect model (per spec):
 *  - GET {endpoint}/api/v1/receivers/{receiver_id}/stream
 *  - Headers: Authorization: Bearer <identity_token>, Accept: text/event-stream,
 *    Cache-Control: no-cache.
 *  - The token is sent ONLY in the Authorization header; never logged, never in
 *    the URL/query.
 *
 * Reliability model: this class handles ONE logical connection attempt and
 * returns why it ended. The caller (foreground service) owns the reconnect loop
 * and exponential backoff.
 *
 * Heartbeat watchdog: OkHttp's read timeout is set to a value larger than 2x the
 * server heartbeat interval (server sends a comment every 30s; we treat ~75s of
 * total silence as a dead connection). A read timeout surfaces as
 * [Disconnect.Transient] and triggers a reconnect with backoff.
 */
class SseClient(
    private val client: OkHttpClient,
) {
    /** Why a connection ended. */
    sealed interface Disconnect {
        /** Coroutine was cancelled; the caller must stop. */
        object Cancelled : Disconnect
        /** HTTP 401/403/404: stop fast retry and enter a diagnosable state. */
        data class Permanent(val httpCode: Int) : Disconnect
        /** EOF, network error, heartbeat timeout, or 5xx: backoff and retry. */
        object Transient : Disconnect
    }

    private val gson = Gson()

    suspend fun runOnce(
        profile: Profile,
        heartbeatTimeoutMs: Long = DEFAULT_HEARTBEAT_TIMEOUT_MS,
        onConnected: () -> Unit,
        onEvent: (NotificationEvent) -> Unit,
        onHeartbeat: () -> Unit,
    ): Disconnect = withContext(Dispatchers.IO) {
        runOnceBlocking(profile, heartbeatTimeoutMs, onConnected, onEvent, onHeartbeat)
    }

    @Suppress("ReturnCount")
    private fun runOnceBlocking(
        profile: Profile,
        heartbeatTimeoutMs: Long,
        onConnected: () -> Unit,
        onEvent: (NotificationEvent) -> Unit,
        onHeartbeat: () -> Unit,
    ): Disconnect {
        val streamUrl = buildStreamUrl(profile) ?: run {
            Logx.w("Cannot build stream url (invalid endpoint/receiver)")
            return Disconnect.Permanent(0)
        }

        // Token goes ONLY into the header below; never logged.
        val request = Request.Builder()
            .url(streamUrl)
            .header("Authorization", "Bearer ${profile.identityToken}")
            .header("Accept", "text/event-stream")
            .header("Cache-Control", "no-cache")
            .get()
            .build()

        // Per-call client with a read timeout used as the heartbeat watchdog.
        val callClient = client.newBuilder()
            .readTimeout(heartbeatTimeoutMs, java.util.concurrent.TimeUnit.MILLISECONDS)
            .build()

        var toClose: Response? = null
        try {
            val resp = callClient.newCall(request).execute()
            toClose = resp
            val code = resp.code
            if (code == 401 || code == 403) {
                Logx.w("Stream rejected: auth failure ($code)")
                return Disconnect.Permanent(code)
            }
            if (code == 404) {
                Logx.w("Stream rejected: receiver not found (404)")
                return Disconnect.Permanent(404)
            }
            if (code !in 200..299) {
                Logx.w("Stream transient failure: HTTP $code")
                return Disconnect.Transient
            }

            val body = resp.body ?: return Disconnect.Transient
            val source = body.source()
            onConnected()

            var eventType: String? = null
            val dataLines = ArrayList<String>()

            while (true) {
                val line = source.readUtf8Line()
                if (line == null) {
                    // Clean EOF from the server -> reconnect.
                    Logx.i("Stream EOF")
                    return Disconnect.Transient
                }
                when {
                    line.isEmpty() -> {
                        if (eventType == "notification" && dataLines.isNotEmpty()) {
                            val payload = dataLines.joinToString("\n")
                            parseNotification(payload)?.let(onEvent)
                        }
                        eventType = null
                        dataLines.clear()
                    }
                    line.startsWith(":") -> {
                        // Comment / heartbeat.
                        onHeartbeat()
                    }
                    line.startsWith("event:", ignoreCase = true) -> {
                        eventType = line.substring(6).trim()
                    }
                    line.startsWith("data:", ignoreCase = true) -> {
                        // Per spec, strip exactly one optional leading space.
                        val raw = line.substring(5)
                        val value = if (raw.startsWith(" ")) raw.substring(1) else raw
                        dataLines.add(value)
                    }
                    // id:/retry: and unknown fields are ignored.
                }
            }
        } catch (_: kotlinx.coroutines.CancellationException) {
            return Disconnect.Cancelled
        } catch (_: SocketTimeoutException) {
            // No bytes for heartbeatTimeoutMs -> dead connection.
            Logx.w("Stream read timeout (heartbeat watchdog) -> reconnect")
            return Disconnect.Transient
        } catch (e: IOException) {
            Logx.w("Stream IO error -> reconnect: ${e.javaClass.simpleName}")
            return Disconnect.Transient
        } catch (e: Throwable) {
            Logx.e("Stream unexpected error", e)
            return Disconnect.Transient
        } finally {
            toClose?.close()
        }
    }

    private fun buildStreamUrl(profile: Profile): String? {
        val base = profile.serverEndpoint.trimEnd('/', ' ')
        if (base.isEmpty() || profile.receiverId.isBlank()) return null
        val rid = URLEncoder.encode(profile.receiverId, "UTF-8")
        return "$base/api/v1/receivers/$rid/stream"
    }

    private fun parseNotification(data: String): NotificationEvent? = try {
        gson.fromJson(data, NotificationEvent::class.java)
    } catch (e: JsonSyntaxException) {
        Logx.w("Malformed notification payload ignored: ${e.message}")
        null
    }

    companion object {
        // Server heartbeat is 30s; 2x = 60s; 75s gives margin before declaring
        // the connection dead.
        const val DEFAULT_HEARTBEAT_TIMEOUT_MS = 75_000L
    }
}
