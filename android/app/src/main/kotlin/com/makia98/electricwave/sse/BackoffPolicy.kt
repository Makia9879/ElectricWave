package com.makia98.electricwave.sse

import kotlin.math.min
import kotlin.random.Random

/**
 * Pure reconnect backoff policy (contract §10.2 / spec 0005/0006). No Android
 * framework dependency, so it is unit-testable on a plain JVM.
 *
 * Delay sequence: **1, 2, 5, 10, 30, 60 seconds**, capped at **300 seconds**,
 * with symmetric ±20% jitter. After the explicit sequence is exhausted, the
 * base stays clamped at the cap.
 *
 * The class is both a pure function of the attempt index
 * ([baseSeconds]/[delaySeconds]) and a small stateful helper
 * ([nextDelaySeconds]/[reset]) for convenience in the reconnect loop.
 *
 * @param sequenceSeconds canonical backoff steps in seconds.
 * @param capSeconds absolute upper bound in seconds (also applied after jitter).
 * @param jitterFraction maximum fractional deviation from the base, in [0,1].
 * @param random source of jitter; inject [Random] with a fixed seed for tests.
 */
class BackoffPolicy(
    private val sequenceSeconds: List<Long> = DEFAULT_SEQUENCE,
    private val capSeconds: Long = DEFAULT_CAP,
    private val jitterFraction: Double = DEFAULT_JITTER_FRACTION,
    private val random: Random = Random,
) {

    /** Base delay for [attempt] (0-indexed), clamped to [capSeconds], no jitter. */
    fun baseSeconds(attempt: Int): Long {
        val idx = attempt.coerceAtLeast(0)
        val seqVal = if (idx < sequenceSeconds.size) {
            sequenceSeconds[idx]
        } else {
            sequenceSeconds.last()
        }
        return min(seqVal, capSeconds)
    }

    /**
     * Jittered delay in milliseconds for [attempt]. Computed in ms (not seconds)
     * so that the ±[jitterFraction] jitter retains sub-second precision even for
     * the 1s step. The result lies within ±[jitterFraction] of
     * [baseSeconds]`*1000`, hard-clamped to `[0, capSeconds*1000]`.
     */
    fun delayMillis(attempt: Int): Long {
        val baseMs = baseSeconds(attempt) * 1000L
        if (baseMs <= 0L) return 0L
        val unit = random.nextDouble() * 2.0 - 1.0 // in [-1, +1]
        val delta = baseMs * unit * jitterFraction
        val raw = (baseMs + delta).toLong()
        return raw.coerceIn(0L, capSeconds * 1000L)
    }

    /** Convenience: jittered delay in whole seconds (floor of [delayMillis]). */
    fun delaySeconds(attempt: Int): Long = delayMillis(attempt) / 1000L

    // ---- stateful convenience for the reconnect loop ----

    private var attempt: Int = 0

    /** Advances the internal counter and returns the next jittered delay (ms). */
    fun nextDelayMillis(): Long {
        val d = delayMillis(attempt)
        attempt += 1
        return d
    }

    /** Current attempt index (0 = first try). */
    fun currentAttempt(): Int = attempt

    /** Resets the internal counter to 0 (next call is the first attempt again). */
    fun reset() {
        attempt = 0
    }

    companion object {
        val DEFAULT_SEQUENCE: List<Long> = listOf(1L, 2L, 5L, 10L, 30L, 60L)
        const val DEFAULT_CAP: Long = 300L
        const val DEFAULT_JITTER_FRACTION: Double = 0.20
    }
}
