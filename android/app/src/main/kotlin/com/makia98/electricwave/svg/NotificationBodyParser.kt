package com.makia98.electricwave.svg

/**
 * Splits a notification body into plain-text and SVG segments.
 *
 * The webhook body is still a string (contract limit applies). When that string
 * contains one or more `<svg>` documents, the detail screen renders those
 * segments as images and leaves the surrounding text as text. Unclosed markup
 * stays text so a failed match never hides content.
 */
object NotificationBodyParser {

    private val svgOpen = Regex("""<svg\b""", RegexOption.IGNORE_CASE)
    private val svgClose = Regex("""</svg\s*>""", RegexOption.IGNORE_CASE)
    private val xmlDeclaration = Regex("""<\?xml[^>]*\?>""", RegexOption.IGNORE_CASE)
    private val doctype = Regex("""<!DOCTYPE[^>]*>""", RegexOption.IGNORE_CASE)
    private val fence = Regex("""```(?:svg|xml|html)?""", RegexOption.IGNORE_CASE)

    fun parse(body: String): List<BodySegment> {
        if (body.isEmpty() || svgOpen.find(body) == null) {
            return if (body.isEmpty()) emptyList() else listOf(BodySegment.Text(body))
        }
        val segments = mutableListOf<BodySegment>()
        var index = 0
        while (index < body.length) {
            val open = svgOpen.find(body, index) ?: break
            if (open.range.first > index) {
                segments += BodySegment.Text(body.substring(index, open.range.first))
            }
            val tagEnd = openingTagEnd(body, open.range.last + 1)
            if (tagEnd < 0) {
                segments += BodySegment.Text(body.substring(open.range.first))
                return coalesce(segments)
            }
            val selfClosing = tagEnd > open.range.first && body[tagEnd - 1] == '/'
            if (selfClosing) {
                segments += BodySegment.Svg(body.substring(open.range.first, tagEnd + 1))
                index = tagEnd + 1
                continue
            }
            val closeEnd = findMatchingClose(body, tagEnd + 1)
            if (closeEnd < 0) {
                segments += BodySegment.Text(body.substring(open.range.first))
                return coalesce(segments)
            }
            segments += BodySegment.Svg(body.substring(open.range.first, closeEnd + 1))
            index = closeEnd + 1
        }
        if (index < body.length) {
            segments += BodySegment.Text(body.substring(index))
        }
        return coalesce(segments)
    }

    /** Inbox / system-notification preview. SVG markup is not dumped as text. */
    fun preview(body: String, maxChars: Int = 160): String {
        val segments = parse(body)
        if (segments.none { it is BodySegment.Svg }) return body
        val text = segments.filterIsInstance<BodySegment.Text>()
            .joinToString(" ") { visibleText(it.value) }
            .replace(Regex("\\s+"), " ")
            .trim()
        val combined = if (text.isEmpty()) SVG_LABEL else "$text $SVG_LABEL"
        if (maxChars <= 1 || combined.length <= maxChars) return combined
        return combined.take(maxChars - 1) + "…"
    }

    /**
     * Text safe to show beside a rendered SVG: drops XML preamble and markdown
     * fences that only wrap the image.
     */
    fun visibleText(raw: String): String =
        raw.replace("\uFEFF", "")
            .replace(xmlDeclaration, "")
            .replace(doctype, "")
            .replace(fence, "")
            .trim()

    fun hasSvg(body: String): Boolean = parse(body).any { it is BodySegment.Svg }

    private fun findMatchingClose(body: String, from: Int): Int {
        var depth = 1
        var cursor = from
        while (cursor < body.length && depth > 0) {
            val open = svgOpen.find(body, cursor)
            val close = svgClose.find(body, cursor) ?: return -1
            if (open != null && open.range.first < close.range.first) {
                depth++
                cursor = open.range.last + 1
            } else {
                depth--
                if (depth == 0) return close.range.last
                cursor = close.range.last + 1
            }
        }
        return -1
    }

    /** Index of the `>` that ends the opening tag, respecting quoted values. */
    private fun openingTagEnd(body: String, from: Int): Int {
        var quote: Char? = null
        var i = from
        while (i < body.length) {
            val c = body[i]
            if (quote != null) {
                if (c == quote) quote = null
            } else {
                when (c) {
                    '"', '\'' -> quote = c
                    '>' -> return i
                }
            }
            i++
        }
        return -1
    }

    private fun coalesce(segments: List<BodySegment>): List<BodySegment> {
        if (segments.size < 2) return segments
        val out = ArrayList<BodySegment>(segments.size)
        for (segment in segments) {
            val prev = out.lastOrNull()
            if (segment is BodySegment.Text && prev is BodySegment.Text) {
                out[out.lastIndex] = BodySegment.Text(prev.value + segment.value)
            } else {
                out += segment
            }
        }
        return out
    }

    const val SVG_LABEL = "［SVG 图示］"
}

sealed class BodySegment {
    data class Text(val value: String) : BodySegment()
    data class Svg(val source: String) : BodySegment()
}
