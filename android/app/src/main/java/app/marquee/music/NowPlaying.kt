package app.marquee.music

import androidx.compose.foundation.border

import androidx.compose.material.icons.automirrored.filled.PlaylistAdd

import app.marquee.ui.CastButton
import kotlinx.coroutines.launch
import app.marquee.ui.initialFocus
import app.marquee.ui.focusRing
import app.marquee.ui.LocalMarquee
import app.marquee.api.models.RadioRequest
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.material.icons.filled.Radio
import androidx.compose.material.icons.filled.DirectionsCar
import androidx.compose.material.icons.filled.Lyrics
import androidx.compose.material.icons.automirrored.filled.QueueMusic
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.layout.systemBarsPadding
import androidx.compose.foundation.layout.fillMaxHeight
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

/** Full-screen Now Playing: artwork or lyrics or the queue, rating, radio, sleep timer and DJ. */
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun NowPlayingScreen(onClose: () -> Unit, onCarMode: (() -> Unit)? = null, onRemote: ((Long) -> Unit)? = null, onOpenPlaylist: ((Long) -> Unit)? = null) {
    val music = LocalMusic.current
    val marquee = LocalMarquee.current
    val now by music.now.collectAsState()
    val source by music.source.collectAsState()
    val castDevice by marquee.cast.device.collectAsState()
    var panel by remember { mutableStateOf(Panel.Art) }
    var radioError by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val n = now
    // Save as playlist: the whole queue (played and upcoming), named after the station or mix.
    var saving by remember { mutableStateOf(false) }
    val snackbar = remember { androidx.compose.material3.SnackbarHostState() }
    fun saveQueue() { if (music.queue.value.isNotEmpty()) saving = true }
    fun startRadio() {
        val id = n?.id ?: return
        scope.launch {
            radioError = runCatching { music.startRadio(RadioRequest(RadioRequest.Seed.ITEM, itemId = id)) }.exceptionOrNull()?.message
        }
    }

    val header: @Composable () -> Unit = {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClick = onClose, modifier = Modifier.focusRing()) { Icon(Icons.Filled.KeyboardArrowDown, "Close Now Playing") }
            Column(Modifier.weight(1f), horizontalAlignment = Alignment.CenterHorizontally) {
                Text(if (source == null) "NOW PLAYING" else "PLAYING FROM", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                source?.let { Text(it, style = MaterialTheme.typography.labelLarge, maxLines = 1, overflow = TextOverflow.Ellipsis) }
                castDevice?.let { Text("Playing on $it", style = MaterialTheme.typography.labelMedium, color = Gold, maxLines = 1) }
            }
            CastButton(MaterialTheme.colorScheme.onSurface)
            // Play on another Marquee app (USER-14): the queue, from this track and position; then pauses here.
            if (onRemote != null) app.marquee.ui.PlayOnButton(tint = MaterialTheme.colorScheme.onSurface, handoff = {
                val q = music.queue.value
                if (q.isEmpty()) null
                else app.marquee.api.models.RemoteCommand(app.marquee.api.models.RemoteCommand.Type.PLAY, itemIds = q.map { it.id },
                    index = music.index.value.coerceIn(0, q.size - 1), startMs = music.position.value.first)
            }, onSent = { music.pause() }, onPlayer = onRemote)
            PanelButton(Icons.Filled.Lyrics, "Lyrics", panel == Panel.Lyrics) { panel = if (panel == Panel.Lyrics) Panel.Art else Panel.Lyrics }
            PanelButton(Icons.AutoMirrored.Filled.QueueMusic, "Up Next", panel == Panel.Queue) { panel = if (panel == Panel.Queue) Panel.Art else Panel.Queue }
        }
    }
    val panelContent: @Composable (Modifier) -> Unit = { m ->
        when {
            n == null -> Box(m, contentAlignment = Alignment.Center) { Text("Nothing playing", color = MaterialTheme.colorScheme.onSurfaceVariant) }
            panel == Panel.Lyrics -> LyricsPanel(n.id, music.position.collectAsState().value.first, music::seek, m)
            panel == Panel.Queue -> QueueList(m, ::saveQueue)
            else -> Box(m, contentAlignment = Alignment.Center) {
                AsyncImage(n.artwork, null, contentScale = ContentScale.Crop,
                    modifier = Modifier.widthIn(max = 420.dp).fillMaxWidth().aspectRatio(1f).clip(RoundedCornerShape(12.dp)).background(Surface2))
            }
        }
    }
    val details: @Composable () -> Unit = {
        if (n != null) Column(horizontalAlignment = Alignment.CenterHorizontally, modifier = Modifier.fillMaxWidth()) {
            n.dj?.let { Text(it.uppercase(), style = MaterialTheme.typography.labelSmall, color = Gold) }
            Text(n.title, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(listOf(n.artist, n.album).filter { it.isNotBlank() }.joinToString(" · "), color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
            QualityBadge(n)
            Transport()
            // Wraps onto a second line on narrow phones.
            androidx.compose.foundation.layout.FlowRow(horizontalArrangement = Arrangement.spacedBy(4.dp, Alignment.CenterHorizontally)) {
                Box(Modifier.height(48.dp), contentAlignment = Alignment.Center) {
                    RatingStars(music.rating.collectAsState().value, music::rate, size = if (marquee.isTv) 22 else 24)
                }
                IconButton(::startRadio, Modifier.focusRing()) { Icon(Icons.Filled.Radio, "Start Radio", tint = MaterialTheme.colorScheme.onSurfaceVariant) }
                IconButton(::saveQueue, Modifier.focusRing()) {
                    Icon(Icons.AutoMirrored.Filled.PlaylistAdd, "Save as playlist", tint = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                SleepButton()
                DJButton()
                CrossfadeButton()
                EqButton()
                // Car mode on phones (the big-button screen).
                if (onCarMode != null && !marquee.isTv) IconButton(onCarMode, Modifier.focusRing()) {
                    Icon(Icons.Filled.DirectionsCar, "Car mode", tint = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            radioError?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
        }
    }

    Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background)) {
        if (marquee.isTv) {
            // TV: artwork, lyrics or the queue on the left, details and controls on the right.
            Column(Modifier.fillMaxSize().padding(horizontal = 48.dp, vertical = 24.dp)) {
                header()
                Row(Modifier.fillMaxSize().padding(top = 12.dp), horizontalArrangement = Arrangement.spacedBy(48.dp), verticalAlignment = Alignment.CenterVertically) {
                    panelContent(Modifier.weight(1f).fillMaxHeight())
                    Box(Modifier.weight(1f), contentAlignment = Alignment.Center) { details() }
                }
            }
        } else {
            Column(Modifier.fillMaxSize().systemBarsPadding().padding(horizontal = 24.dp, vertical = 12.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                header()
                panelContent(Modifier.weight(1f).fillMaxWidth().padding(vertical = 16.dp))
                details()
            }
        }
        androidx.compose.material3.SnackbarHost(snackbar, Modifier.align(Alignment.BottomCenter).padding(bottom = 24.dp))
    }
    if (saving) SaveAsPlaylistDialog(defaultPlaylistTitle(source), music.queue.value.map { it.id }) { pl ->
        saving = false
        if (pl != null) scope.launch {
            val r = snackbar.showSnackbar("Saved “${pl.title}”", actionLabel = if (onOpenPlaylist != null) "Open" else null,
                duration = androidx.compose.material3.SnackbarDuration.Long)
            if (r == androidx.compose.material3.SnackbarResult.ActionPerformed) onOpenPlaylist?.invoke(pl.id)
        }
    }
}

private enum class Panel { Art, Lyrics, Queue }

@Composable
private fun PanelButton(icon: androidx.compose.ui.graphics.vector.ImageVector, label: String, on: Boolean, onClick: () -> Unit) {
    IconButton(onClick = onClick, modifier = Modifier.focusRing()) { Icon(icon, label, tint = if (on) Gold else MaterialTheme.colorScheme.onSurface) }
}

/** Scrubber, times and the play controls. */
@Composable
private fun Transport() {
    val music = LocalMusic.current
    val playing by music.playing.collectAsState()
    val (pos, dur) = music.position.collectAsState().value
    val shuffle by music.shuffle.collectAsState()
    val repeat by music.repeat.collectAsState()
    var scrub by remember { mutableStateOf<Float?>(null) }
    Slider(
        value = scrub ?: if (dur > 0) pos.toFloat() / dur else 0f,
        onValueChange = { scrub = it },
        onValueChangeFinished = { scrub?.let { music.seek((it * dur).toLong()) }; scrub = null },
        modifier = Modifier.widthIn(max = 480.dp).padding(top = 8.dp).semantics { contentDescription = "Position" },
    )
    Row(Modifier.widthIn(max = 480.dp).fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(formatTime(pos), style = MaterialTheme.typography.labelSmall)
        Text("-" + formatTime((dur - pos).coerceAtLeast(0)), style = MaterialTheme.typography.labelSmall)
    }
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp), modifier = Modifier.padding(vertical = 4.dp)) {
        IconButton(onClick = music::toggleShuffle, modifier = Modifier.focusRing()) { Icon(Icons.Filled.Shuffle, "Shuffle", tint = if (shuffle) Gold else MaterialTheme.colorScheme.onSurfaceVariant) }
        IconButton(onClick = music::previous, modifier = Modifier.focusRing()) { Icon(Icons.Filled.SkipPrevious, "Previous track", Modifier.size(36.dp)) }
        IconButton(onClick = music::toggle, modifier = Modifier.focusRing().size(68.dp).clip(RoundedCornerShape(34.dp)).background(Gold).initialFocus(LocalMarquee.current.isTv)) {
            Icon(if (playing) Icons.Filled.Pause else Icons.Filled.PlayArrow, if (playing) "Pause" else "Play", Modifier.size(38.dp), tint = MaterialTheme.colorScheme.onPrimary)
        }
        IconButton(onClick = music::next, modifier = Modifier.focusRing()) { Icon(Icons.Filled.SkipNext, "Next track", Modifier.size(36.dp)) }
        IconButton(onClick = music::cycleRepeat, modifier = Modifier.focusRing()) {
            Icon(if (repeat == Player.REPEAT_MODE_ONE) Icons.Filled.RepeatOne else Icons.Filled.Repeat, "Repeat",
                tint = if (repeat == Player.REPEAT_MODE_OFF) MaterialTheme.colorScheme.onSurfaceVariant else Gold)
        }
    }
}

/** Up Next: tap to jump, with the DJ's picks marked. */
@Composable
private fun QueueList(modifier: Modifier, onSave: () -> Unit) {
    val music = LocalMusic.current
    val queue by music.queue.collectAsState()
    val index by music.index.collectAsState()
    val state = rememberLazyListState()
    LaunchedEffect(Unit) { state.scrollToItem(index.coerceAtLeast(0) + 1) } // past the header
    LazyColumn(modifier, state = state) {
        item {
            Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(if (queue.size == 1) "1 track" else "${queue.size} tracks", Modifier.weight(1f), style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant)
                androidx.compose.material3.TextButton(onSave, Modifier.focusRing()) {
                    Icon(Icons.AutoMirrored.Filled.PlaylistAdd, null)
                    Text("Save as playlist", Modifier.padding(start = 6.dp))
                }
            }
        }
        itemsIndexed(queue) { i, q ->
            Row(Modifier.fillMaxWidth().focusCard({ music.jump(i) }).padding(vertical = 10.dp, horizontal = 8.dp),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                AsyncImage(q.artwork, null, contentScale = ContentScale.Crop, modifier = Modifier.size(40.dp).clip(RoundedCornerShape(4.dp)).background(Surface2))
                Column(Modifier.weight(1f)) {
                    Text(q.title, color = if (i == index) Gold else MaterialTheme.colorScheme.onSurface, maxLines = 1, overflow = TextOverflow.Ellipsis,
                        fontWeight = if (i == index) FontWeight.Bold else FontWeight.Normal)
                    Text(listOfNotNull(q.artist.ifBlank { null }, q.dj).joinToString(" · "), style = MaterialTheme.typography.bodySmall,
                        color = if (q.dj != null) Gold else MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                }
            }
        }
    }
}

/** Show audio quality (MUSIC-23): the track's format, a Hi-Res tag, and what's streamed when it isn't the original. */
@Composable
private fun QualityBadge(n: MusicController.Now) {
    val music = LocalMusic.current
    val on by music.showQuality.collectAsState()
    val f = n.audio
    if (!on || f == null) return
    val streamed by MusicService.streamed.collectAsState()
    val text = AudioQuality.label(f) + (streamed[n.id]?.let { " → $it" } ?: "")
    Row(Modifier.padding(top = 4.dp).semantics(mergeDescendants = true) { contentDescription = "Audio quality: $text" + if (AudioQuality.isHiRes(f)) ", Hi-Res" else "" },
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        if (AudioQuality.isHiRes(f)) Text("Hi-Res", Modifier.clip(RoundedCornerShape(4.dp)).background(Gold).padding(horizontal = 5.dp, vertical = 1.dp),
            style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onPrimary, fontWeight = FontWeight.Bold, maxLines = 1)
        Text(text, Modifier.clip(RoundedCornerShape(4.dp)).border(1.dp, MaterialTheme.colorScheme.outline, RoundedCornerShape(4.dp)).padding(horizontal = 6.dp, vertical = 1.dp),
            style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}
