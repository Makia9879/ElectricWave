package com.makia98.electricwave.sse

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.random.Random

/**
 * Pure-JVM tests for [BackoffPolicy]. Verifies the canonical sequence
 * (1/2/5/10/30/60), the 300s cap, the ±20% jitter bounds, and reset() behavior
 * (contract §10.2).
 */
class BackoffPolicyTest {

    @Test
    fun `base sequence matches 1 2 5 10 30 60 for in-range attempts`() {
        val p = BackoffPolicy()
        assertEquals(1L, p.baseSeconds(0))
        assertEquals(2L, p.baseSeconds(1))
        assertEquals(5L, p.baseSeconds(2))
        assertEquals(10L, p.baseSeconds(3))
        assertEquals(30L, p.baseSeconds(4))
        assertEquals(60L, p.baseSeconds(5))
    }

    @Test
    fun `base clamps to last sequence value beyond the explicit steps`() {
        val p = BackoffPolicy()
        assertEquals(60L, p.baseSeconds(6))
        assertEquals(60L, p.baseSeconds(100))
    }

    @Test
    fun `base never exceeds the 300s cap`() {
        val p = BackoffPolicy()
        repeat(200) { attempt ->
            assertTrue("attempt $attempt", p.baseSeconds(attempt) <= 300L)
        }
    }

    @Test
    fun `custom sequence beyond cap is clamped to cap`() {
        // A sequence that would exceed the cap must be hard-clamped.
        val p = BackoffPolicy(sequenceSeconds = listOf(1000L), capSeconds = 300L)
        assertEquals(300L, p.baseSeconds(0))
    }

    @Test
    fun `jitter stays within plus or minus 20 percent of base`() {
        val p = BackoffPolicy(random = Random(12345))
        repeat(6) { attempt ->
            val baseMs = p.baseSeconds(attempt) * 1000L
            val low = (baseMs * 0.8).toLong()
            val high = (baseMs * 1.2).toLong()
            repeat(50) {
                val d = p.delayMillis(attempt)
                assertTrue("attempt=$attempt delay=$d baseMs=$baseMs", d in low..high)
            }
        }
    }

    @Test
    fun `zero jitter makes delay equal base in milliseconds`() {
        val p = BackoffPolicy(jitterFraction = 0.0, random = Random(0))
        val expected = listOf(1_000L, 2_000L, 5_000L, 10_000L, 30_000L, 60_000L)
        expected.forEachIndexed { attempt, ms ->
            assertEquals(ms, p.delayMillis(attempt))
            assertEquals(ms / 1000L, p.delaySeconds(attempt))
        }
    }

    @Test
    fun `delay never exceeds the cap across many attempts`() {
        val p = BackoffPolicy(random = Random(99))
        repeat(500) { attempt ->
            val d = p.delayMillis(attempt)
            assertTrue("attempt=$attempt delay=$d", d <= 300_000L)
        }
    }

    @Test
    fun `nextDelayMillis increments attempt and reset returns to start`() {
        val p = BackoffPolicy(random = Random(7))
        assertEquals(0, p.currentAttempt())
        // First call corresponds to attempt 0 (base 1s = 1000ms); ±20% => [800,1200].
        val firstBaseMs = p.baseSeconds(0) * 1000L
        assertTrue(p.nextDelayMillis() in (firstBaseMs * 0.8).toLong()..(firstBaseMs * 1.2).toLong())
        assertEquals(1, p.currentAttempt())

        // Walk a few steps.
        repeat(3) { p.nextDelayMillis() }
        assertEquals(4, p.currentAttempt())

        p.reset()
        assertEquals(0, p.currentAttempt())
        val againBaseMs = p.baseSeconds(0) * 1000L
        assertTrue(p.nextDelayMillis() in (againBaseMs * 0.8).toLong()..(againBaseMs * 1.2).toLong())
    }

    @Test
    fun `deterministic with a fixed seed`() {
        val a = BackoffPolicy(random = Random(42))
        val b = BackoffPolicy(random = Random(42))
        repeat(10) { attempt ->
            assertEquals(a.delaySeconds(attempt), b.delaySeconds(attempt))
        }
    }
}
