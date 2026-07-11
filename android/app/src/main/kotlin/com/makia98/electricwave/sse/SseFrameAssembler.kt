package com.makia98.electricwave.sse

import com.google.gson.Gson
import com.google.gson.JsonSyntaxException

/**
 * One completed SSE event block: the parsed `id:` value, the `event:` name, and
 * the joined `data:` payload. Comment/heartbeat lines never produce a frame.
 */
data class SseFrame(
    val id: Long?,
    val event: String?,
    val data: String,
)

/**
 * Pure, line-driven SSE block assembler (no OkHttp / no Android). Unit-testable.
 *
 * Feed raw lines one at a time with [consume]. A blank line dispatches the
 * accumulated block and returns it as a [SseFrame] (or null for an empty block).
 * Comment lines (`:`) and unknown fields are ignored here; the caller is
 * responsible for treating `:` lines as heartbeats (see [SseClient]).
 *
 * Field parsing follows the SSE spec: `field: value`, stripping exactly one
 * optional leading space from the value. Multiple `data:` lines are joined with
 * `\n`. `id:` is parsed as a 64-bit integer (non-numeric → ignored).
 */
class SseFrameAssembler {
    private var id: Long? = null
    private var event: String? = null
    private val dataLines = ArrayList<String>()

    /** Returns a completed [SseFrame] when [line] is the block terminator. */
    fun consume(line: String): SseFrame? {
        if (line.isEmpty()) {
            val frame = if (dataLines.isNotEmpty() || id != null || event != null) {
                SseFrame(id = id, event = event, data = dataLines.joinToString("\n"))
            } else {
                null
            }
            id = null
            event = null
            dataLines.clear()
            return frame
        }
        // Per spec, lines starting with U+003A are comments and ignored.
        if (line.startsWith(":")) return null

        val (field, value) = splitField(line)
        when (field.lowercase()) {
            "id" -> value.toLongOrNull()?.let { id = it }
            "event" -> event = value.trim()
            "data" -> dataLines.add(value)
            // retry: and any unknown field are ignored.
        }
        return null
    }

    private fun splitField(line: String): Pair<String, String> {
        val colon = line.indexOf(':')
        if (colon < 0) return line to "" // no colon: field is the whole line, value empty
        val field = line.substring(0, colon)
        var value = line.substring(colon + 1)
        if (value.startsWith(" ")) value = value.substring(1) // strip one optional leading space
        return field to value
    }
}

/**
 * Pure JSON payload parser for SSE event blocks (no OkHttp / no Android).
 * Unit-testable with a plain [Gson] instance.
 */
object SsePayloadParser {

    /**
     * Parses a `notification` data payload. The `id:` line value ([blockId]) is
     * authoritative per contract §2.1 (id line and JSON event_id must match); it
     * overrides the JSON field when present.
     */
    fun parseNotification(gson: Gson, data: String, blockId: Long?): NotificationEvent? {
        return try {
            val parsed = gson.fromJson(data, NotificationEvent::class.java) ?: return null
            val authoritativeId = blockId ?: parsed.eventId
            if (authoritativeId != parsed.eventId) parsed.copy(eventId = authoritativeId) else parsed
        } catch (e: JsonSyntaxException) {
            null
        }
    }

    fun parseInfo(gson: Gson, data: String): InfoEvent? = try {
        gson.fromJson(data, InfoEvent::class.java)
    } catch (e: JsonSyntaxException) {
        null
    }

    fun parseBacklogGap(gson: Gson, data: String): BacklogGapEvent? = try {
        gson.fromJson(data, BacklogGapEvent::class.java)
    } catch (e: JsonSyntaxException) {
        null
    }
}
