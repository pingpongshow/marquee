package app.marquee.ui

import androidx.annotation.OptIn
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.text.Cue
import androidx.media3.common.util.UnstableApi
import androidx.media3.ui.CaptionStyleCompat
import androidx.media3.ui.SubtitleView
import app.marquee.api.models.MeUpdate
import app.marquee.api.models.SubtitleStyle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Subtitle colours offered (PLAY-20). */
private val subtitleColours = listOf("White" to "#FFFFFF", "Yellow" to "#FFFF00", "Cyan" to "#00FFFF", "Green" to "#00FF00")

/** How much bigger than ExoPlayer's default each size is. */
private fun sizeScale(s: SubtitleStyle.PropertySize?) = when (s) {
    SubtitleStyle.PropertySize.SMALL -> 0.75f
    SubtitleStyle.PropertySize.LARGE -> 1.35f
    SubtitleStyle.PropertySize.HUGE -> 1.75f
    else -> 1f
}

private fun parseColour(hex: String?): Int =
    runCatching { android.graphics.Color.parseColor(hex ?: "#FFFFFF") }.getOrDefault(android.graphics.Color.WHITE)

/**
 * Applies the person's subtitle appearance (PLAY-20) to ExoPlayer's subtitle view: size, colour,
 * a shadow, outline or box behind the text, and raised above the controls. Text subtitles
 * arrive as WebVTT, so their own styling gives way to this; burned-in ones are styled by the server.
 */
@OptIn(UnstableApi::class)
fun applySubtitleStyle(view: SubtitleView, style: SubtitleStyle?) {
    val fg = parseColour(style?.color)
    val black = android.graphics.Color.BLACK
    val transparent = android.graphics.Color.TRANSPARENT
    val caption = when (style?.background) {
        SubtitleStyle.Background.NONE -> CaptionStyleCompat(fg, transparent, transparent, CaptionStyleCompat.EDGE_TYPE_DROP_SHADOW, black, null)
        SubtitleStyle.Background.TRANSLUCENT -> CaptionStyleCompat(fg, 0x99000000.toInt(), transparent, CaptionStyleCompat.EDGE_TYPE_NONE, black, null)
        SubtitleStyle.Background.OPAQUE -> CaptionStyleCompat(fg, black, transparent, CaptionStyleCompat.EDGE_TYPE_NONE, black, null)
        else -> CaptionStyleCompat(fg, transparent, transparent, CaptionStyleCompat.EDGE_TYPE_OUTLINE, black, null)
    }
    view.setApplyEmbeddedStyles(false)
    view.setApplyEmbeddedFontSizes(false)
    view.setStyle(caption)
    view.setFractionalTextSize(SubtitleView.DEFAULT_TEXT_SIZE_FRACTION * sizeScale(style?.propertySize))
    view.setBottomPaddingFraction(if (style?.position == SubtitleStyle.Position.RAISED) 0.22f else SubtitleView.DEFAULT_BOTTOM_PADDING_FRACTION)
}

/**
 * Settings → Subtitle appearance (PLAY-20): size, colour, background and position, with a live
 * preview. Saved to the person's preferences, so every Marquee app uses it.
 */
@kotlin.OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@OptIn(UnstableApi::class)
@Composable
fun SubtitleAppearanceScreen() {
    val marquee = LocalMarquee.current
    val me by marquee.me.collectAsState()
    val scope = rememberCoroutineScope()
    var status by remember { mutableStateOf<String?>(null) }
    val u = me ?: return
    val style = u.preferences.subtitleStyle ?: SubtitleStyle()
    val size = style.propertySize ?: SubtitleStyle.PropertySize.MEDIUM
    val colour = (style.color ?: "#FFFFFF").uppercase()
    val bg = style.background ?: SubtitleStyle.Background.OUTLINE
    val pos = style.position ?: SubtitleStyle.Position.BOTTOM

    // The whole preferences object goes back, so nothing else changes.
    fun save(next: SubtitleStyle) {
        val full = SubtitleStyle(next.propertySize ?: size, next.color ?: colour, next.background ?: bg, next.position ?: pos)
        marquee.updated(u.copy(preferences = u.preferences.copy(subtitleStyle = full))) // shows at once
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.auth.updateMe(MeUpdate(preferences = u.preferences.copy(subtitleStyle = full))) } }
                .onSuccess { marquee.updated(it); status = "Saved" }
                .onFailure { marquee.updated(u); status = "Couldn't save: ${it.message}" }
        }
    }

    @Composable
    fun Label(t: String) = Text(t, Modifier.padding(top = 8.dp), style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Bold, color = Gold)

    @Composable
    fun <T> Choices(options: List<Pair<String, T>>, current: T, focusFirst: Boolean = false, onPick: (T) -> Unit) =
        androidx.compose.foundation.layout.FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            options.forEachIndexed { i, (label, v) ->
                FilterChip(v == current, { onPick(v) }, { Text(label) },
                    Modifier.focusRing(RoundedCornerShape(8.dp)).initialFocus(focusFirst && marquee.isTv && i == 0))
            }
        }

    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(sidePadding), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text("Subtitle appearance", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
        Text("How subtitles look in every Marquee app. Styled subtitles (ASS) keep their own look.",
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        // Live preview: ExoPlayer's own subtitle view over a dark "scene".
        Box(
            Modifier.fillMaxWidth().height(if (marquee.isTv) 220.dp else 190.dp).clip(RoundedCornerShape(12.dp))
                .background(Brush.verticalGradient(listOf(Color(0xFF3A5A7A), Color(0xFF8A9AA8), Color(0xFFDADADA))))
                .semantics { contentDescription = "Subtitle preview"; stateDescription = "${size.value}, $colour, ${bg.value}, ${pos.value}" }
                .testTag("subtitlePreview"),
        ) {
            AndroidView({ ctx ->
                SubtitleView(ctx).apply { setCues(listOf(Cue.Builder().setText("This is how your subtitles will look.").build())) }
            }, Modifier.fillMaxSize(), update = { applySubtitleStyle(it, SubtitleStyle(size, colour, bg, pos)) })
        }
        Label("Size")
        Choices(listOf("Small" to SubtitleStyle.PropertySize.SMALL, "Medium" to SubtitleStyle.PropertySize.MEDIUM,
            "Large" to SubtitleStyle.PropertySize.LARGE, "Huge" to SubtitleStyle.PropertySize.HUGE), size, focusFirst = true) { save(style.copy(propertySize = it)) }
        Label("Colour")
        Row(horizontalArrangement = Arrangement.spacedBy(14.dp), verticalAlignment = Alignment.CenterVertically) {
            subtitleColours.forEach { (name, hex) ->
                val on = hex == colour
                Box(
                    Modifier.size(40.dp).focusRing(CircleShape).clip(CircleShape).background(Color(parseColour(hex)))
                        .border(if (on) 3.dp else 1.dp, if (on) Gold else Color.White.copy(alpha = 0.3f), CircleShape)
                        .semantics { contentDescription = name; selected = on }
                        .clickable { save(style.copy(color = hex)) },
                )
            }
        }
        Label("Background")
        Choices(listOf("None" to SubtitleStyle.Background.NONE, "Outline" to SubtitleStyle.Background.OUTLINE,
            "Translucent" to SubtitleStyle.Background.TRANSLUCENT, "Opaque" to SubtitleStyle.Background.OPAQUE), bg) { save(style.copy(background = it)) }
        Label("Position")
        Choices(listOf("Bottom" to SubtitleStyle.Position.BOTTOM, "Raised" to SubtitleStyle.Position.RAISED), pos) { save(style.copy(position = it)) }
        OutlinedButton({ save(SubtitleStyle(SubtitleStyle.PropertySize.MEDIUM, "#FFFFFF", SubtitleStyle.Background.OUTLINE, SubtitleStyle.Position.BOTTOM)) },
            Modifier.padding(top = 8.dp).focusRing()) { Text("Reset to default") }
        status?.let { Text(it, color = if (it == "Saved") Gold else MaterialTheme.colorScheme.error) }
    }
}
