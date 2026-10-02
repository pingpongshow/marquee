package app.marquee.music

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.QueueMusic
import androidx.compose.material.icons.filled.Repeat
import androidx.compose.material.icons.filled.RepeatOne
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.SkipPrevious
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.media3.common.Player
import app.marquee.ui.Gold
import app.marquee.ui.LocalMusic
import app.marquee.ui.Surface2
import app.marquee.ui.focusCard
import app.marquee.ui.formatTime
import coil3.compose.AsyncImage

/** The bar above the tabs while music plays. */
@Composable
fun MiniPlayer(onOpen: () -> Unit) {
    val music = LocalMusic.current
    val now by music.now.collectAsState()
    val playing by music.playing.collectAsState()
    val (pos, dur) = music.position.collectAsState().value
    val n = now ?: return
    Surface(color = MaterialTheme.colorScheme.surface) {
        Column {
            LinearProgressIndicator(progress = { if (dur > 0) pos.toFloat() / dur else 0f }, modifier = Modifier.fillMaxWidth().height(2.dp), color = Gold, trackColor = Surface2)
            Row(Modifier.fillMaxWidth().clickable(onClick = onOpen).semantics { contentDescription = "Open Now Playing" }.padding(horizontal = 12.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                AsyncImage(n.artwork, null, contentScale = ContentScale.Crop, modifier = Modifier.size(44.dp).clip(RoundedCornerShape(6.dp)).background(Surface2))
                Column(Modifier.weight(1f)) {
                    Text(n.title, maxLines = 1, overflow = TextOverflow.Ellipsis, fontWeight = FontWeight.SemiBold)
                    Text(n.artist, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                IconButton(onClick = music::toggle) { Icon(if (playing) Icons.Filled.Pause else Icons.Filled.PlayArrow, if (playing) "Pause" else "Play") }
                IconButton(onClick = music::next) { Icon(Icons.Filled.SkipNext, "Next track") }
            }
        }
    }
}

/** Full-screen Now Playing with the queue. */
@Composable
fun NowPlayingScreen(onClose: () -> Unit) {
    val music = LocalMusic.current
    val now by music.now.collectAsState()
    val playing by music.playing.collectAsState()
    val (pos, dur) = music.position.collectAsState().value
    val source by music.source.collectAsState()
    val queue by music.queue.collectAsState()
    val index by music.index.collectAsState()
    val shuffle by music.shuffle.collectAsState()
    val repeat by music.repeat.collectAsState()
    var showQueue by remember { mutableStateOf(false) }
    var scrub by remember { mutableStateOf<Float?>(null) }
    val n = now
    Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background)) {
        Column(Modifier.fillMaxSize().padding(24.dp), horizontalAlignment = Alignment.CenterHorizontally) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                IconButton(onClick = onClose) { Icon(Icons.Filled.KeyboardArrowDown, "Close Now Playing") }
                Column(Modifier.weight(1f), horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(if (source == null) "NOW PLAYING" else "PLAYING FROM", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    source?.let { Text(it, style = MaterialTheme.typography.labelLarge, maxLines = 1) }
                }
                IconButton(onClick = { showQueue = !showQueue }) { Icon(Icons.Filled.QueueMusic, "Up Next", tint = if (showQueue) Gold else MaterialTheme.colorScheme.onSurface) }
            }
            if (showQueue) {
                LazyColumn(Modifier.weight(1f).fillMaxWidth()) {
                    itemsIndexed(queue) { i, q ->
                        Row(Modifier.fillMaxWidth().focusCard({ music.jump(i) }).padding(vertical = 10.dp, horizontal = 8.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            Column {
                                Text(q.title, color = if (i == index) Gold else MaterialTheme.colorScheme.onSurface, maxLines = 1)
                                Text(q.artist, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                            }
                        }
                    }
                }
            } else if (n != null) {
                Spacer(Modifier.weight(0.3f))
                AsyncImage(n.artwork, null, contentScale = ContentScale.Crop,
                    modifier = Modifier.widthIn(max = 360.dp).fillMaxWidth().aspectRatio(1f).clip(RoundedCornerShape(12.dp)).background(Surface2))
                Text(n.title, Modifier.padding(top = 20.dp), style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(listOf(n.artist, n.album).filter { it.isNotBlank() }.joinToString(" · "), color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                Slider(
                    value = scrub ?: if (dur > 0) pos.toFloat() / dur else 0f,
                    onValueChange = { scrub = it },
                    onValueChangeFinished = { scrub?.let { music.seek((it * dur).toLong()) }; scrub = null },
                    modifier = Modifier.widthIn(max = 480.dp).padding(top = 12.dp),
                )
                Row(Modifier.widthIn(max = 480.dp).fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                    Text(formatTime(pos), style = MaterialTheme.typography.labelSmall)
                    Text("-" + formatTime((dur - pos).coerceAtLeast(0)), style = MaterialTheme.typography.labelSmall)
                }
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(20.dp), modifier = Modifier.padding(top = 8.dp)) {
                    IconButton(onClick = music::toggleShuffle) { Icon(Icons.Filled.Shuffle, "Shuffle", tint = if (shuffle) Gold else MaterialTheme.colorScheme.onSurfaceVariant) }
                    IconButton(onClick = music::previous) { Icon(Icons.Filled.SkipPrevious, "Previous track", Modifier.size(36.dp)) }
                    IconButton(onClick = music::toggle, modifier = Modifier.size(72.dp).clip(RoundedCornerShape(36.dp)).background(Gold)) {
                        Icon(if (playing) Icons.Filled.Pause else Icons.Filled.PlayArrow, if (playing) "Pause" else "Play", Modifier.size(40.dp), tint = MaterialTheme.colorScheme.onPrimary)
                    }
                    IconButton(onClick = music::next) { Icon(Icons.Filled.SkipNext, "Next track", Modifier.size(36.dp)) }
                    IconButton(onClick = music::cycleRepeat) {
                        Icon(if (repeat == Player.REPEAT_MODE_ONE) Icons.Filled.RepeatOne else Icons.Filled.Repeat, "Repeat",
                            tint = if (repeat == Player.REPEAT_MODE_OFF) MaterialTheme.colorScheme.onSurfaceVariant else Gold)
                    }
                }
                Spacer(Modifier.weight(0.5f))
            } else {
                Spacer(Modifier.weight(1f))
                Text("Nothing playing", color = MaterialTheme.colorScheme.onSurfaceVariant)
                Spacer(Modifier.weight(1f))
            }
        }
    }
}
