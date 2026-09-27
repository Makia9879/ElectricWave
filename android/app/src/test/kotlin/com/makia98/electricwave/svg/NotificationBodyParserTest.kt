package com.makia98.electricwave.svg

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NotificationBodyParserTest {

    @Test
    fun `plain text is unchanged`() {
        val body = "订单 123 已完成支付"
        val segments = NotificationBodyParser.parse(body)
        assertEquals(listOf(BodySegment.Text(body)), segments)
        assertEquals(body, NotificationBodyParser.preview(body))
        assertFalse(NotificationBodyParser.hasSvg(body))
    }

    @Test
    fun `empty body yields no segments`() {
        assertTrue(NotificationBodyParser.parse("").isEmpty())
    }

    @Test
    fun `svg document is one segment and preview hides markup`() {
        val svg = """<svg viewBox="0 0 10 10"><circle cx="5" cy="5" r="4"/></svg>"""
        assertEquals(listOf(BodySegment.Svg(svg)), NotificationBodyParser.parse(svg))
        assertEquals(NotificationBodyParser.SVG_LABEL, NotificationBodyParser.preview(svg))
    }

    @Test
    fun `mixed text and svg keeps surrounding copy`() {
        val svg = """<svg viewBox="0 0 8 8"></svg>"""
        val body = "更新说明\n$svg\n请查收"
        val segments = NotificationBodyParser.parse(body)
        assertEquals(
            listOf(
                BodySegment.Text("更新说明\n"),
                BodySegment.Svg(svg),
                BodySegment.Text("\n请查收"),
            ),
            segments,
        )
        assertEquals("更新说明 请查收 ${NotificationBodyParser.SVG_LABEL}", NotificationBodyParser.preview(body))
    }

    @Test
    fun `multiple and nested svg documents`() {
        val inner = """<svg viewBox="0 0 2 2"></svg>"""
        val outer = """<svg viewBox="0 0 10 10">$inner</svg>"""
        val second = """<SVG viewBox="0 0 4 4"></SVG>"""
        val segments = NotificationBodyParser.parse("$outer\n$second")
        assertEquals(listOf(BodySegment.Svg(outer), BodySegment.Text("\n"), BodySegment.Svg(second)), segments)
    }

    @Test
    fun `self closing svg is a segment`() {
        val svg = """<svg viewBox="0 0 1 1" data-label="a>b"/>"""
        assertEquals(listOf(BodySegment.Svg(svg)), NotificationBodyParser.parse("  $svg".trim()))
        val spaced = """<svg viewBox="0 0 1 1" />"""
        assertEquals(listOf(BodySegment.Svg(spaced)), NotificationBodyParser.parse(spaced))
    }

    @Test
    fun `unclosed svg stays text`() {
        val body = "hello <svg viewBox=\"0 0 1 1\">"
        assertEquals(listOf(BodySegment.Text(body)), NotificationBodyParser.parse(body))
        assertEquals(body, NotificationBodyParser.preview(body))
    }

    @Test
    fun `svgish word is not treated as a tag`() {
        val body = "call <svgish> later"
        assertEquals(listOf(BodySegment.Text(body)), NotificationBodyParser.parse(body))
    }

    @Test
    fun `preamble and fences are hidden beside the image but kept in source`() {
        val svg = """<svg viewBox="0 0 4 4"></svg>"""
        val body = "<?xml version=\"1.0\"?>\n```svg\n$svg\n```"
        val segments = NotificationBodyParser.parse(body)
        assertTrue(segments.first() is BodySegment.Text)
        assertEquals(svg, (segments[1] as BodySegment.Svg).source)
        assertEquals("", NotificationBodyParser.visibleText((segments.first() as BodySegment.Text).value))
        assertEquals(NotificationBodyParser.SVG_LABEL, NotificationBodyParser.preview(body))
    }

    @Test
    fun `quoted greater-than inside opening tag does not end the tag`() {
        val svg = """<svg data-x="a>b" viewBox="0 0 3 3"><rect width="3" height="3"/></svg>"""
        assertEquals(listOf(BodySegment.Svg(svg)), NotificationBodyParser.parse(svg))
    }
}
