package app.marquee.music

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.systemBarsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.FavoriteBorder
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Radio
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.SkipPrevious
import androidx.compose.material.icons.filled.Today
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import app.marquee.api.apis.ItemsApi
import app.marquee.api.models.ItemType
import app.marquee.api.models.LibraryType
import app.marquee.api.models.RadioRequest
import app.marquee.ui.Gold
import app.marquee.ui.LocalMarquee
import app.marquee.ui.LocalMusic
import coil3.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private val CarGrey = Color(0xFFB5B5BD)
private val CarTile = Color(0xFF1C1C22)

/**
 * Car mode for phones: black, high contrast and big targets. Large artwork,
 * huge previous / play-pause / next, a like button and quick starts (Library Radio, a daily
 * mix, recently played, shuffle all). Keeps the screen on and works either way up.
 */
@Composable
fun CarModeScreen(libraryId: Long?, onExit: () -> Unit) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    val scope = rememberCoroutineScope()
    val view = LocalView.current
    DisposableEffect(Unit) {
        view.keepScreenOn = true
        onDispose { view.keepScreenOn = false }
    }
    // The music library the quick starts draw on: the one it was opened from, or the first.
    val library by produceState(libraryId) {
        if (value == null) value = withContext(Dispatchers.IO) {
            runCatching { marquee.libraries.listLibraries().firstOrNull { it.type == LibraryType.MUSIC }?.id }.getOrNull()
        }
    }
    var busy by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }

    fun quick(name: String, work: suspend (Long) -> Unit) {
        val lib = library ?: run { error = "There's no music library."; return }
        if (busy != null) return
        busy = name
        error = null
        scope.launch {
            runCatching { work(lib) }.onFailure { error = it.message ?: "Couldn't start $name" }
            busy = null
        }
    }
    val tiles = listOf(
        Triple("Library Radio", Icons.Filled.Radio) { quick("Library Radio") { music.startRadio(RadioRequest(RadioRequest.Seed.LIBRARY, libraryId = it)) } },
        Triple("Daily Mix", Icons.Filled.Today) {
            quick("Daily Mix") { lib ->
                val mix = withContext(Dispatchers.IO) { marquee.music.musicMixes(lib) }.firstOrNull { it.items.isNotEmpty() }
                    ?: throw IllegalStateException("No mixes yet: they appear once you've played some music.")
                music.playStation(mix)
            }
        },
        Triple("Recently Played", Icons.Filled.History) {
            quick("Recently Played") { lib ->
                val tracks = withContext(Dispatchers.IO) {
                    marquee.items.listLibraryItems(lib, ItemType.TRACK, ItemsApi.SortListLibraryItems._VIEWED, limit = 100).items
                }.filter { it.lastViewedAt != null }
                if (tracks.isEmpty()) throw IllegalStateException("Nothing played yet.")
                music.play(tracks, 0, source = "Recently Played")
            }
        },
        Triple("Shuffle All", Icons.Filled.Shuffle) {
            quick("Shuffle All") { lib ->
                val tracks = withContext(Dispatchers.IO) {
                    marquee.items.listLibraryItems(lib, ItemType.TRACK, ItemsApi.SortListLibraryItems.RANDOM, limit = 200).items
                }
                music.play(tracks, 0, source = "Shuffle All")
            }
        },
    )

    BoxWithConstraints(Modifier.fillMaxSize().background(Color.Black).systemBarsPadding().semantics { contentDescription = "Car mode" }) {
        val landscape = maxWidth > maxHeight
        Column(Modifier.fillMaxSize().padding(horizontal = 20.dp, vertical = 12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                TextButton(onExit, Modifier.height(56.dp)) {
                    Icon(Icons.Filled.Close, null, Modifier.size(32.dp), tint = Color.White)
                    Text("Exit", Modifier.padding(start = 6.dp), color = Color.White, fontSize = 20.sp, fontWeight = FontWeight.SemiBold)
                }
                Box(Modifier.weight(1f))
                Text("CAR MODE", color = Gold, style = MaterialTheme.typography.labelLarge, fontWeight = FontWeight.Bold)
            }
            if (landscape) Row(Modifier.weight(1f).fillMaxWidth().padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(24.dp)) {
                CarArtwork(Modifier.fillMaxHeight().aspectRatio(1f, matchHeightConstraintsFirst = true))
                Column(Modifier.weight(1f).fillMaxHeight(), verticalArrangement = Arrangement.SpaceEvenly) {
                    CarNowPlaying()
                    CarControls(80, 104)
                    CarTiles(tiles, busy, perRow = 4)
                }
            } else Column(Modifier.weight(1f).fillMaxWidth().padding(top = 8.dp), verticalArrangement = Arrangement.SpaceEvenly) {
                CarArtwork(Modifier.weight(1f, fill = false).aspectRatio(1f).align(Alignment.CenterHorizontally))
                CarNowPlaying()
                CarControls(80, 104)
                CarTiles(tiles, busy, perRow = 2)
            }
            error?.let { Text(it, Modifier.padding(top = 6.dp), color = MaterialTheme.colorScheme.error, fontSize = 16.sp) }
        }
    }
}

@Composable
private fun CarArtwork(modifier: Modifier) {
    val now by LocalMusic.current.now.collectAsState()
    Box(modifier.clip(RoundedCornerShape(16.dp)).background(CarTile)) {
        now?.artwork?.let { AsyncImage(it, null, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize()) }
    }
}

@Composable
private fun CarNowPlaying() {
    val now by LocalMusic.current.now.collectAsState()
    val n = now
    Column(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        Text(n?.title ?: "Nothing playing", color = Color.White, fontSize = 28.sp, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(n?.artist?.ifBlank { null } ?: "Pick something below", color = CarGrey, fontSize = 20.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** Previous, play/pause and next, plus Like (a loved rating). */
@Composable
private fun CarControls(side: Int, middle: Int) {
    val music = LocalMusic.current
    val playing by music.playing.collectAsState()
    val rating by music.rating.collectAsState()
    val now by music.now.collectAsState()
    val loved = (rating ?: 0.0) >= 10.0
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceEvenly, verticalAlignment = Alignment.CenterVertically) {
        IconButton(music::previous, Modifier.size(side.dp)) { Icon(Icons.Filled.SkipPrevious, "Previous track", Modifier.size((side * 0.75).dp), tint = Color.White) }
        IconButton(music::toggle, Modifier.size(middle.dp).clip(CircleShape).background(Gold)) {
            Icon(if (playing) Icons.Filled.Pause else Icons.Filled.PlayArrow, if (playing) "Pause" else "Play", Modifier.size((middle * 0.6).dp), tint = Color.Black)
        }
        IconButton(music::next, Modifier.size(side.dp)) { Icon(Icons.Filled.SkipNext, "Next track", Modifier.size((side * 0.75).dp), tint = Color.White) }
        IconButton({ music.rate(if (loved) null else 10.0) }, Modifier.size((side * 0.8).dp), enabled = now != null) {
            Icon(if (loved) Icons.Filled.Favorite else Icons.Filled.FavoriteBorder, if (loved) "Unlike" else "Like",
                Modifier.size((side * 0.5).dp), tint = if (loved) Gold else Color.White)
        }
    }
}

@Composable
private fun CarTiles(tiles: List<Triple<String, ImageVector, () -> Unit>>, busy: String?, perRow: Int) {
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        tiles.chunked(perRow).forEach { row ->
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                row.forEach { (label, icon, onClick) ->
                    Row(
                        Modifier.weight(1f).height(72.dp).clip(RoundedCornerShape(14.dp)).background(CarTile).clickable(onClick = onClick)
                            .semantics(mergeDescendants = true) { role = Role.Button }.padding(horizontal = 14.dp),
                        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        if (busy == label) CircularProgressIndicator(Modifier.size(28.dp), color = Gold, strokeWidth = 3.dp)
                        else Icon(icon, null, Modifier.size(30.dp), tint = Gold)
                        Text(label, color = Color.White, fontSize = 18.sp, fontWeight = FontWeight.SemiBold, maxLines = 2)
                    }
                }
            }
        }
    }
}
