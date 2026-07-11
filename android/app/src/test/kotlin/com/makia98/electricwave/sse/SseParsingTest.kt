package com.makia98.electricwave.sse

import com.google.gson.Gson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pure-JVM tests for the SSE block assembler and payload parser (contract §2):
 * `id:` / `event:` / `data:` line parsing, block dispatch, and the
 * notification/info/backlog_gap event split.
 */
class SseParsingTest {

    private val gson = Gson()

    @Test
    fun `assembler collects id event and data then dispatches on blank line`() {
        val a = SseFrameAssembler()
        assertNull(a.consume("id: 42"))
        assertNull(a.consume("event: notification"))
        assertNull(a.consume("data: {\"type\":\"notification\",\"event_id\":42}"))
        val frame = a.consume("")
        assertNotNull(frame)
        assertEquals(42L, frame!!.id)
        assertEquals("notification", frame.event)
        assertEquals("{\"type\":\"notification\",\"event_id\":42}", frame.data)
    }

    @Test
    fun `assembler ignores comment lines and unknown fields`() {
        val a = SseFrameAssembler()
        assertNull(a.consume(": heartbeat"))
        assertNull(a.consume("retry: 5000"))
        assertNull(a.consume("event: notification"))
        assertNull(a.consume("data: hello"))
        val frame = a.consume("")
        assertNotNull(frame)
        assertNull(frame!!.id)
        assertEquals("notification", frame.event)
        assertEquals("hello", frame.data)
    }

    @Test
    fun `assembler joins multiple data lines with newline`() {
        val a = SseFrameAssembler()
        a.consume("event: notification")
        a.consume("data: line1")
        a.consume("data: line2")
        val frame = a.consume("")
        assertEquals("line1\nline2", frame!!.data)
    }

    @Test
    fun `assembler parses id as long and ignores non-numeric id`() {
        val a = SseFrameAssembler()
        a.consume("id: abc")
        a.consume("data: x")
        val frame = a.consume("")
        assertNull(frame!!.id)

        val b = SseFrameAssembler()
        b.consume("id:99")
        b.consume("data: x")
        val f2 = b.consume("")
        assertEquals(99L, f2!!.id)
    }

    @Test
    fun `blank block produces no frame`() {
        val a = SseFrameAssembler()
        assertNull(a.consume(""))
    }

    @Test
    fun `parser injects id-line value into notification event`() {
        val data = "{\"type\":\"notification\",\"notification_id\":\"ntf_1\",\"event_id\":42,\"title\":\"hi\"}"
        val ev = SsePayloadParser.parseNotification(gson, data, blockId = 42L)
        assertNotNull(ev)
        assertEquals(42L, ev!!.eventId)
        assertEquals("ntf_1", ev.notificationId)
        assertEquals("hi", ev.title)
    }

    @Test
    fun `parser prefers id-line value when json event_id differs`() {
        // Contract §2.1: id line and JSON event_id must match. When they don't,
        // the id line is authoritative.
        val data = "{\"type\":\"notification\",\"event_id\":42}"
        val ev = SsePayloadParser.parseNotification(gson, data, blockId = 77L)
        assertEquals(77L, ev!!.eventId)
    }

    @Test
    fun `parser falls back to json event_id when id line absent`() {
        val data = "{\"type\":\"notification\",\"event_id\":9}"
        val ev = SsePayloadParser.parseNotification(gson, data, blockId = null)
        assertEquals(9L, ev!!.eventId)
    }

    @Test
    fun `parser returns null on malformed json`() {
        assertNull(SsePayloadParser.parseNotification(gson, "{not json", blockId = 1L))
        assertNull(SsePayloadParser.parseInfo(gson, "{not json"))
        assertNull(SsePayloadParser.parseBacklogGap(gson, "{not json"))
    }

    @Test
    fun `parser parses info control event`() {
        val data = "{\"type\":\"info\",\"acked_event_id\":40,\"oldest_unacked_event_id\":41,\"newest_event_id\":50,\"backlog_count\":10}"
        val info = SsePayloadParser.parseInfo(gson, data)
        assertNotNull(info)
        assertEquals(40L, info!!.ackedEventId)
        assertEquals(41L, info.oldestUnackedEventId)
        assertEquals(50L, info.newestEventId)
        assertEquals(10, info.backlogCount)
    }

    @Test
    fun `parser parses info with null backlog fields`() {
        val data = "{\"type\":\"info\",\"acked_event_id\":null,\"oldest_unacked_event_id\":null,\"newest_event_id\":null,\"backlog_count\":0}"
        val info = SsePayloadParser.parseInfo(gson, data)
        assertNull(info!!.ackedEventId)
        assertEquals(0, info.backlogCount)
    }

    @Test
    fun `parser parses backlog_gap control event`() {
        val data = "{\"type\":\"backlog_gap\",\"from_event_id\":41,\"to_event_id\":44,\"reason\":\"retention_exceeded\"}"
        val gap = SsePayloadParser.parseBacklogGap(gson, data)
        assertNotNull(gap)
        assertEquals(41L, gap!!.fromEventId)
        assertEquals(44L, gap.toEventId)
        assertEquals("retention_exceeded", gap.reason)
    }

    @Test
    fun `full notification frame assembly and parse end to end`() {
        val a = SseFrameAssembler()
        a.consume("id: 7")
        a.consume("event: notification")
        a.consume("data: {\"type\":\"notification\",\"notification_id\":\"ntf_x\",\"event_id\":7,\"title\":\"t\",\"body\":\"b\",\"priority\":\"high\"}")
        val frame = a.consume("")!!
        assertEquals("notification", frame.event)
        val ev = SsePayloadParser.parseNotification(gson, frame.data, frame.id)!!
        assertEquals(7L, ev.eventId)
        assertEquals("ntf_x", ev.notificationId)
        assertEquals("high", ev.priority)
        assertTrue(ev.body == "b")
    }
}
