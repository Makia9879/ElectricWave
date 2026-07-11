package com.makia98.electricwave.sse

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * Pure-JVM tests for [SseClient.parseRetryAfter] (contract §10.2: honor
 * Retry-After / 429, cap at 300s).
 */
class SseRetryAfterTest {

    @Test
    fun `parses delta seconds`() {
        assertEquals(30L, SseClient.parseRetryAfter("30"))
        assertEquals(0L, SseClient.parseRetryAfter("0"))
    }

    @Test
    fun `trims whitespace`() {
        assertEquals(15L, SseClient.parseRetryAfter("  15 "))
    }

    @Test
    fun `null or blank header returns null`() {
        assertNull(SseClient.parseRetryAfter(null))
        assertNull(SseClient.parseRetryAfter(""))
        assertNull(SseClient.parseRetryAfter("   "))
    }

    @Test
    fun `non-numeric value returns null`() {
        assertNull(SseClient.parseRetryAfter("Wed, 21 Oct 2015 07:28:00 GMT"))
        assertNull(SseClient.parseRetryAfter("abc"))
    }

    @Test
    fun `capped at 300 seconds`() {
        assertEquals(300L, SseClient.parseRetryAfter("600"))
        assertEquals(300L, SseClient.parseRetryAfter("99999"))
    }

    @Test
    fun `negative value returns null`() {
        assertNull(SseClient.parseRetryAfter("-5"))
    }
}
