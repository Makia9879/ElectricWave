package com.makia98.electricwave.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pure-JVM tests for the ack/cursor dedup decision (contract §10.2):
 * eventId <= ackedEventId -> drop; eventId > ackedEventId -> accept and advance.
 */
class AckCursorTest {

    @Test
    fun `fresh cursor has value 0 and accepts event_id 1`() {
        val c = AckCursor()
        assertEquals(0L, c.value)
        assertTrue(c.shouldAccept(1L))
    }

    @Test
    fun `equal event id is a duplicate and is dropped`() {
        val c = AckCursor(initial = 5L)
        assertFalse(c.shouldAccept(5L))
    }

    @Test
    fun `older event id is a duplicate and is dropped`() {
        val c = AckCursor(initial = 5L)
        assertFalse(c.shouldAccept(4L))
        assertFalse(c.shouldAccept(1L))
    }

    @Test
    fun `newer event id is accepted and advances the cursor`() {
        val c = AckCursor(initial = 5L)
        assertTrue(c.shouldAccept(8L))
        assertEquals(8L, c.advance(8L))
        assertEquals(8L, c.value)
    }

    @Test
    fun `advance never decreases the cursor`() {
        val c = AckCursor(initial = 10L)
        assertEquals(10L, c.advance(3L))
        assertEquals(10L, c.advance(10L))
        assertEquals(15L, c.advance(15L))
        assertEquals(15L, c.value)
    }

    @Test
    fun `accept-then-advance rejects the just-accepted id on replay`() {
        val c = AckCursor(initial = 0L)
        // First delivery of event 42.
        assertTrue(c.shouldAccept(42L))
        c.advance(42L)
        // Reconnect replays event 42 -> must drop.
        assertFalse(c.shouldAccept(42L))
        assertEquals(42L, c.value)
    }

    @Test
    fun `reset clears the cursor back to 0`() {
        val c = AckCursor(initial = 99L)
        c.reset()
        assertEquals(0L, c.value)
        assertTrue(c.shouldAccept(1L))
    }

    @Test
    fun `negative initial value is clamped to 0`() {
        val c = AckCursor(initial = -5L)
        assertEquals(0L, c.value)
    }
}
