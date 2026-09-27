package com.makia98.electricwave.svg

import android.graphics.Bitmap
import android.graphics.Canvas
import com.caverock.androidsvg.PreserveAspectRatio
import com.caverock.androidsvg.SVG

/**
 * Rasterizes an SVG document into a bitmap sized to [widthPx].
 *
 * AndroidSVG does not execute script or event handlers. No
 * [com.caverock.androidsvg.SVGExternalFileResolver] is registered, so
 * `http(s):`, `file:`, and asset references in `<image>`, fonts, or CSS are
 * not loaded. Inline `data:` images remain part of the document.
 */
object SvgRasterizer {

    const val MAX_PX = 2048

    fun rasterize(source: String, widthPx: Int, dpi: Float = 96f): Bitmap? {
        if (source.isBlank() || widthPx <= 0) return null
        return try {
            val svg = SVG.getFromString(source)
            if (dpi > 0f) svg.setRenderDPI(dpi)
            val aspect = svg.documentAspectRatio.takeIf { it.isFinite() && it > 0.05f && it < 20f }
                ?: 1.6f
            val width = widthPx.coerceIn(1, MAX_PX)
            val height = (width / aspect).toInt().coerceIn(1, MAX_PX)
            svg.setDocumentWidth(width.toFloat())
            svg.setDocumentHeight(height.toFloat())
            svg.setDocumentPreserveAspectRatio(PreserveAspectRatio.LETTERBOX)
            val bitmap = Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888)
            svg.renderToCanvas(Canvas(bitmap))
            bitmap
        } catch (_: Throwable) {
            null
        }
    }
}
