package app.marquee.ui

import androidx.activity.compose.BackHandler
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Replay
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage

/** How long Up Next counts down before the next video plays. */
const val UP_NEXT_SECONDS = 10

/** One side of the Up Next screen: the video that just finished, or the one about to play. */
data class UpNextEntry(val show: String?, val title: String, val label: String?, val imageUrl: String?, val overview: String? = null)

/** "SEASON 5 - EPISODE 4", "SPECIAL 3" for season 0, or what's known of it. */
fun episodeLabel(season: Int?, episode: Int?): String? = when {
    season == 0 && episode != null -> "SPECIAL $episode"
    season != null && episode != null -> "SEASON $season - EPISODE $episode"
    episode != null -> "EPISODE $episode"
    else -> null
}

/**
 * Up Next, after a video has finished: full screen over the show's art, the one just watched
 * (select to watch it again), the next one with a countdown ring (select to play it now; it
 * plays when the ring runs out) and the next one's details. Back or Cancel leaves the player.
 * Wide screens and TVs have the two cards and the details side by side, with the D-pad on
 * the next one; phones in portrait stack them. [held] stops the ring (the rating card is in
 * use); [extra] sits under the details.
 */
@Composable
fun UpNextScreen(previous: UpNextEntry, next: UpNextEntry, backdropUrl: String?, seconds: Int, held: Boolean,
    onPlayNext: () -> Unit, onReplay: () -> Unit, onCancel: () -> Unit, extra: (@Composable () -> Unit)? = null) {
    val marquee = LocalMarquee.current
    BackHandler(onBack = onCancel)
    Box(Modifier.fillMaxSize().background(Ink).semantics { contentDescription = "Up next" }) {
        if (backdropUrl != null) AsyncImage(backdropUrl, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop, alpha = 0.55f)
        Box(Modifier.fillMaxSize().background(Brush.horizontalGradient(0f to Color.Black.copy(alpha = 0.9f), 0.55f to Color.Black.copy(alpha = 0.6f), 1f to Color.Black.copy(alpha = 0.85f))))
        Box(Modifier.fillMaxSize().background(Brush.verticalGradient(0.5f to Color.Transparent, 1f to Color.Black.copy(alpha = 0.8f))))
        BoxWithConstraints(Modifier.fillMaxSize()) {
            val width = maxWidth
            val wide = width > maxHeight && width >= 560.dp
            val nextCard = @Composable { m: Modifier -> NextCard(next, seconds, held, autoFocus = marquee.isTv, onClick = onPlayNext, modifier = m) }
            val details = @Composable { m: Modifier -> Details(next, onCancel, extra, m) }
            if (wide) Row(
                Modifier.fillMaxSize().padding(horizontal = if (marquee.isTv) 56.dp else 32.dp, vertical = 24.dp),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(if (marquee.isTv) 40.dp else 28.dp),
            ) {
                PreviousCard(previous, onReplay, Modifier.width(width * 0.17f))
                nextCard(Modifier.width(width * 0.32f))
                details(Modifier.weight(1f))
            }
            else Column(
                Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = 20.dp, vertical = 28.dp),
                verticalArrangement = Arrangement.spacedBy(24.dp),
            ) {
                nextCard(Modifier.fillMaxWidth())
                details(Modifier.fillMaxWidth())
                PreviousCard(previous, onReplay, Modifier.width(width * 0.5f))
            }
        }
    }
}

@Composable
private fun Caption(text: String) =
    Text(text.uppercase(), Modifier.padding(bottom = 8.dp), color = Color.White.copy(alpha = 0.7f), style = MaterialTheme.typography.labelMedium,
        fontWeight = FontWeight.Bold, letterSpacing = 1.2.sp)

/** A 16:9 still with a dark scrim, and [overlay] in its centre. */
@Composable
private fun Still(url: String?, title: String, modifier: Modifier, overlay: @Composable () -> Unit) {
    Box(modifier.aspectRatio(16f / 9f).background(Brush.linearGradient(listOf(Color(0xFF2A2A36), Color(0xFF15151B)))), contentAlignment = Alignment.Center) {
        if (url != null) AsyncImage(url, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
        else Text(title, Modifier.align(Alignment.BottomStart).padding(8.dp), color = Color.White.copy(alpha = 0.8f), style = MaterialTheme.typography.labelMedium, maxLines = 2)
        Box(Modifier.fillMaxSize().background(Color.Black.copy(alpha = 0.35f)))
        overlay()
    }
}

/** The video just watched: select to watch it again. */
@Composable
private fun PreviousCard(e: UpNextEntry, onReplay: () -> Unit, modifier: Modifier) {
    Column(modifier) {
        Caption("Previous")
        Still(e.imageUrl, e.title, Modifier.fillMaxWidth().focusCard(onReplay).semantics { contentDescription = "Watch again: ${e.title}" }) {
            Icon(Icons.Filled.Replay, null, Modifier.size(44.dp).background(Color.Black.copy(alpha = 0.5f), CircleShape).padding(8.dp), tint = Color.White)
        }
        e.show?.let { Text(it, Modifier.padding(top = 8.dp), color = Color.White.copy(alpha = 0.7f), style = MaterialTheme.typography.labelLarge, maxLines = 1, overflow = TextOverflow.Ellipsis) }
        Text(e.title, Modifier.padding(top = if (e.show == null) 8.dp else 0.dp), color = Color.White, style = MaterialTheme.typography.bodyMedium,
            fontWeight = FontWeight.SemiBold, maxLines = 2, overflow = TextOverflow.Ellipsis)
        e.label?.let { Text(it, color = Color.White.copy(alpha = 0.6f), style = MaterialTheme.typography.labelSmall, letterSpacing = 0.8.sp, maxLines = 1) }
    }
}

/** The video about to play, with the countdown ring: select to play it now. */
@Composable
private fun NextCard(e: UpNextEntry, seconds: Int, held: Boolean, autoFocus: Boolean, onClick: () -> Unit, modifier: Modifier) {
    Column(modifier) {
        Caption("Playing next")
        Still(e.imageUrl, e.title, Modifier.fillMaxWidth().initialFocus(autoFocus).focusCard(onClick, RoundedCornerShape(10.dp))
            .semantics { contentDescription = "Play next" }) {
            CountdownRing(seconds, held)
        }
    }
}

/** A ring that empties as the seconds run out, with the seconds in it (a play icon while held). */
@Composable
private fun CountdownRing(seconds: Int, held: Boolean) {
    val progress by animateFloatAsState((seconds.toFloat() / UP_NEXT_SECONDS).coerceIn(0f, 1f), tween(1000, easing = LinearEasing), label = "countdown")
    Box(
        Modifier.size(96.dp).background(Color.Black.copy(alpha = 0.45f), CircleShape).semantics {
            contentDescription = "Up next countdown"
            stateDescription = if (held) "Paused" else "$seconds seconds"
        },
        contentAlignment = Alignment.Center,
    ) {
        Canvas(Modifier.fillMaxSize().padding(5.dp)) {
            val stroke = Stroke(width = 6.dp.toPx(), cap = StrokeCap.Round)
            drawArc(Color.White.copy(alpha = 0.25f), 0f, 360f, useCenter = false, style = stroke)
            drawArc(Gold, -90f, 360f * progress, useCenter = false, style = stroke)
        }
        if (held) Icon(Icons.Filled.PlayArrow, null, Modifier.size(48.dp), tint = Color.White)
        else Text("$seconds", color = Color.White, fontSize = 40.sp, fontWeight = FontWeight.Bold)
    }
}

/** The next video's show, title, season and episode and overview, and Cancel. */
@Composable
private fun Details(e: UpNextEntry, onCancel: () -> Unit, extra: (@Composable () -> Unit)?, modifier: Modifier) {
    Column(modifier, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(e.show ?: e.title, color = Color.White, style = MaterialTheme.typography.headlineLarge, fontWeight = FontWeight.Bold,
            maxLines = 2, overflow = TextOverflow.Ellipsis)
        if (e.show != null) Text(e.title, color = Color.White, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.SemiBold,
            maxLines = 2, overflow = TextOverflow.Ellipsis)
        e.label?.let { Text(it, color = Gold, style = MaterialTheme.typography.labelLarge, fontWeight = FontWeight.Bold, letterSpacing = 1.sp) }
        e.overview?.takeIf { it.isNotBlank() }?.let {
            Text(it, Modifier.padding(top = 6.dp), color = Color.White.copy(alpha = 0.8f), style = MaterialTheme.typography.bodyMedium,
                maxLines = 4, overflow = TextOverflow.Ellipsis)
        }
        OutlinedButton(onCancel, Modifier.padding(top = 12.dp).focusRing()) { Text("Cancel", color = Color.White) }
        extra?.invoke()
    }
}
