package com.makia98.electricwave.data

/**
 * Pure ack/cursor dedup logic (contract §10.2). No Android framework dependency
 * so it is unit-testable on a plain JVM.
 *
 * Holds the highest event_id the client has acknowledged (persisted). A
 * notification event is accepted only when its `eventId` is strictly greater
 * than the cursor; otherwise it is a replay duplicate and is dropped.
 *
 * The value `0` means "nothing acked yet" (no event has event_id <= 0, since
 * event_ids start at 1 per contract §1).
 *
 * Persistence is the caller's responsibility (see [AckCursorStore]); this class
 * only owns the dedup/advance decision so it can be tested in isolation.
 */
class AckCursor(initial: Long = 0L) {

    /** Highest acked event_id, or 0 if none. Monotonically non-decreasing. */
    var value: Long = initial.coerceAtLeast(0L)
        private set

    /**
     * True iff [eventId] is newer than the cursor and should be processed.
     * Returns false (drop) when [eventId] <= [value] (duplicate or stale).
     */
    fun shouldAccept(eventId: Long): Boolean = eventId > value

    /**
     * Advances the cursor to [eventId] if it is newer and returns the new value.
     * Safe to call with any event_id; never decreases the cursor.
     */
    fun advance(eventId: Long): Long {
        if (eventId > value) value = eventId
        return value
    }

    /** Resets to "nothing acked" (used when clearing local state). */
    fun reset() {
        value = 0L
    }
}
