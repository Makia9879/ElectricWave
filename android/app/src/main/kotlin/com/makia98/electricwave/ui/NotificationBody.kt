package com.makia98.electricwave.ui

import android.graphics.Bitmap
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import com.makia98.electricwave.svg.BodySegment
import com.makia98.electricwave.svg.NotificationBodyParser
import com.makia98.electricwave.svg.SvgRasterizer

/**
 * Renders notification body text, and any inline `<svg>` documents as images.
 * Parse or raster failures fall back to the original text so content is never
 * dropped.
 */
@Composable
fun NotificationBody(body: String, modifier: Modifier = Modifier) {
    if (body.isBlank()) {
        Text("(无正文)", style = MaterialTheme.typography.bodyLarge, modifier = modifier)
        return
    }
    val segments = remember(body) { NotificationBodyParser.parse(body) }
    if (segments.none { it is BodySegment.Svg }) {
        Text(body, style = MaterialTheme.typography.bodyLarge, modifier = modifier)
        return
    }
    var showSource by remember(body) { mutableStateOf(false) }
    Column(modifier = modifier, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        for (segment in segments) {
            when (segment) {
                is BodySegment.Text -> {
                    val text = NotificationBodyParser.visibleText(segment.value)
                    if (text.isNotEmpty()) {
                        Text(text, style = MaterialTheme.typography.bodyLarge)
                    }
                }
                is BodySegment.Svg -> SvgBlock(segment.source)
            }
        }
        TextButton(onClick = { showSource = !showSource }) {
            Text(if (showSource) "隐藏原文" else "查看原文")
        }
        if (showSource) {
            Text(
                text = body,
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
            )
        }
    }
}

@Composable
private fun SvgBlock(source: String) {
    val density = LocalDensity.current
    val dpi = LocalContext.current.resources.displayMetrics.xdpi
    BoxWithConstraints(modifier = Modifier.fillMaxWidth()) {
        if (!maxWidth.value.isFinite() || maxWidth <= 0.dp) return@BoxWithConstraints
        val targetPx = with(density) { maxWidth.roundToPx() }.coerceIn(1, SvgRasterizer.MAX_PX)
        val bitmap = remember(source, targetPx, dpi) {
            SvgRasterizer.rasterize(source, targetPx, dpi)
        }
        if (bitmap == null) {
            Text(
                "SVG 无法渲染，已显示原文",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.error,
            )
            Text(
                text = source,
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
            )
        } else {
            SvgImage(bitmap)
        }
    }
}

@Composable
private fun SvgImage(bitmap: Bitmap) {
    val image = remember(bitmap) { bitmap.asImageBitmap() }
    val ratio = bitmap.width.toFloat() / bitmap.height.coerceAtLeast(1)
    val shape = RoundedCornerShape(12.dp)
    // White plate: notification SVGs are usually authored with black strokes,
    // which disappear on a dark theme surface.
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(Color.White)
            .border(1.dp, MaterialTheme.colorScheme.outlineVariant, shape)
            .padding(8.dp),
    ) {
        Image(
            bitmap = image,
            contentDescription = "通知中的 SVG 图示",
            modifier = Modifier
                .fillMaxWidth()
                .aspectRatio(ratio),
            contentScale = ContentScale.Fit,
        )
    }
}
