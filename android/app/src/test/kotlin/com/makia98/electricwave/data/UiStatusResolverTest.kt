package com.makia98.electricwave.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pure-JVM tests for [UiStatusResolver] — the §10.3 status mapping table. Covers
 * all six UiStatus values and the resolution priority (paused > unavailable >
 * needs-authorization > backlog-pending > receiving > reconnecting).
 */
class UiStatusResolverTest {

    private val ok = DiagnosticFlags() // everything open

    @Test
    fun `disabled profile is paused regardless of run state`() {
        val rs = RunState(status = ConnectionStatus.AUTH_FAILED)
        assertEquals(UiStatus.PAUSED, UiStatusResolver.resolve(false, rs, ok))
    }

    @Test
    fun `auth failed maps to unavailable`() {
        val rs = RunState(status = ConnectionStatus.AUTH_FAILED)
        assertEquals(UiStatus.UNAVAILABLE, UiStatusResolver.resolve(true, rs, ok))
    }

    @Test
    fun `not found maps to unavailable`() {
        val rs = RunState(status = ConnectionStatus.NOT_FOUND)
        assertEquals(UiStatus.UNAVAILABLE, UiStatusResolver.resolve(true, rs, ok))
    }

    @Test
    fun `config error maps to unavailable`() {
        val rs = RunState(status = ConnectionStatus.CONFIG_ERROR)
        assertEquals(UiStatus.UNAVAILABLE, UiStatusResolver.resolve(true, rs, ok))
    }

    @Test
    fun `connected with no backlog is receiving`() {
        val rs = RunState(status = ConnectionStatus.CONNECTED, backlogPending = false)
        assertEquals(UiStatus.RECEIVING, UiStatusResolver.resolve(true, rs, ok))
    }

    @Test
    fun `connecting without backlog is reconnecting`() {
        val rs = RunState(status = ConnectionStatus.CONNECTING)
        assertEquals(UiStatus.RECONNECTING, UiStatusResolver.resolve(true, rs, ok))
    }

    @Test
    fun `backoff without backlog is reconnecting`() {
        val rs = RunState(status = ConnectionStatus.BACKOFF)
        assertEquals(UiStatus.RECONNECTING, UiStatusResolver.resolve(true, rs, ok))
    }

    @Test
    fun `disconnected is reconnecting`() {
        val rs = RunState(status = ConnectionStatus.DISCONNECTED)
        assertEquals(UiStatus.RECONNECTING, UiStatusResolver.resolve(true, rs, ok))
    }

    @Test
    fun `backlog pending takes priority over connected`() {
        val rs = RunState(status = ConnectionStatus.CONNECTED, backlogPending = true, backlogCount = 3)
        assertEquals(UiStatus.BACKLOG_PENDING, UiStatusResolver.resolve(true, rs, ok))
    }

    @Test
    fun `backlog pending takes priority over backoff`() {
        val rs = RunState(status = ConnectionStatus.BACKOFF, backlogPending = true)
        assertEquals(UiStatus.BACKLOG_PENDING, UiStatusResolver.resolve(true, rs, ok))
    }

    // ---- authorization priority ----

    @Test
    fun `notification permission denied maps to needs authorization`() {
        val flags = ok.copy(postPermissionGranted = false)
        val rs = RunState(status = ConnectionStatus.CONNECTED)
        assertEquals(UiStatus.NEEDS_AUTHORIZATION, UiStatusResolver.resolve(true, rs, flags))
    }

    @Test
    fun `app notifications disabled maps to needs authorization`() {
        val flags = ok.copy(appNotificationsEnabled = false)
        val rs = RunState(status = ConnectionStatus.CONNECTED)
        assertEquals(UiStatus.NEEDS_AUTHORIZATION, UiStatusResolver.resolve(true, rs, flags))
    }

    @Test
    fun `default channel closed maps to needs authorization`() {
        val flags = ok.copy(defaultChannelOpen = false)
        val rs = RunState(status = ConnectionStatus.CONNECTED)
        assertEquals(UiStatus.NEEDS_AUTHORIZATION, UiStatusResolver.resolve(true, rs, flags))
    }

    @Test
    fun `foreground channel closed maps to needs authorization`() {
        val flags = ok.copy(foregroundChannelOpen = false)
        val rs = RunState(status = ConnectionStatus.CONNECTED)
        assertEquals(UiStatus.NEEDS_AUTHORIZATION, UiStatusResolver.resolve(true, rs, flags))
    }

    @Test
    fun `needs authorization beats backlog pending`() {
        val flags = ok.copy(urgentChannelOpen = false)
        val rs = RunState(status = ConnectionStatus.CONNECTED, backlogPending = true, backlogCount = 5)
        assertEquals(UiStatus.NEEDS_AUTHORIZATION, UiStatusResolver.resolve(true, rs, flags))
    }

    @Test
    fun `unavailable beats needs authorization`() {
        val flags = ok.copy(postPermissionGranted = false)
        val rs = RunState(status = ConnectionStatus.NOT_FOUND)
        assertEquals(UiStatus.UNAVAILABLE, UiStatusResolver.resolve(true, rs, flags))
    }

    @Test
    fun `paused beats unavailable`() {
        val flags = ok.copy(postPermissionGranted = false)
        val rs = RunState(status = ConnectionStatus.AUTH_FAILED)
        assertEquals(UiStatus.PAUSED, UiStatusResolver.resolve(false, rs, flags))
    }

    @Test
    fun `needsAuthorization detects any blocked flag`() {
        assertTrue(UiStatusResolver.needsAuthorization(ok.copy(appNotificationsEnabled = false)))
        assertTrue(UiStatusResolver.needsAuthorization(ok.copy(defaultChannelOpen = false)))
        assertFalse(UiStatusResolver.needsAuthorization(ok))
    }
}
