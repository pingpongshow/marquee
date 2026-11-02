package app.marquee.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.models.ItemSummary
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** How many top songs an artist's page shows; More opens the rest. */
internal const val TOP_SONGS_SHOWN = 15
private const val TOP_SONGS_PAGE = 100

/** The top of an artist's page (MUSIC-24): your favourites, songs you rated well and what's popular, best first. */
@Composable
fun TopSongsSection(nav: NavHostController, artistId: Long, artist: String, songs: List<ItemSummary>, total: Int) {
    val music = LocalMusic.current
    Column {
        Text("Top Songs", Modifier.padding(horizontal = sidePadding, vertical = 4.dp), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
        songs.forEachIndexed { i, t -> TopSongRow(t, i) { music.play(songs, i, source = "$artist – Top Songs") } }
        if (total > songs.size) TextButton({ nav.navigate("topsongs/$artistId") }, Modifier.padding(horizontal = sidePadding).focusRing()) { Text("More") }
    }
}

@Composable
private fun TopSongRow(t: ItemSummary, i: Int, modifier: Modifier = Modifier, onPlay: () -> Unit) {
    val inRow = rememberInRowFocus()
    ListItem(
        headlineContent = { Text(t.title, maxLines = 2, overflow = TextOverflow.Ellipsis) },
        supportingContent = { t.parentTitle?.let { Text(it, maxLines = 1, overflow = TextOverflow.Ellipsis) } },
        leadingContent = { Text("${i + 1}", color = MaterialTheme.colorScheme.onSurfaceVariant) },
        trailingContent = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                FavoriteButton(t, Modifier.inRowFirst(inRow), small = true)
                app.marquee.music.TrackRating(t)
                app.marquee.music.TrackRadioButton(t, Modifier.padding(end = 8.dp))
                Text(t.durationMs?.let { formatTime(it) } ?: "", maxLines = 1, softWrap = false)
            }
        },
        colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
        modifier = modifier.rowFocus(inRow).focusCard(onPlay),
    )
}

/** All of an artist's Top Songs, a page at a time as you scroll. */
@Composable
fun TopSongsScreen(nav: NavHostController, artistId: Long) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    val artist by cachedLoad("topsongs-$artistId-artist") { withContext(Dispatchers.IO) { runCatching { marquee.items.getItem(artistId).title }.getOrNull() } }
    // Kept while on the back stack, so coming back keeps the list's place.
    val songs = rememberCached("topsongs-$artistId") { mutableStateListOf<ItemSummary>() }
    var total by rememberCached("topsongs-$artistId-total") { mutableIntStateOf(-1) }
    var opened by rememberLastOpened("topsongs-$artistId")
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    fun loadMore() {
        if (loading || (total >= 0 && songs.size >= total)) return
        loading = true
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.items.listTopSongs(artistId, offset = songs.size, limit = TOP_SONGS_PAGE) } }
                .onSuccess { page ->
                    val next = appendUnseen(songs.toList(), page.items) { it.id }
                    // A page that adds nothing is the end, whatever the count says.
                    val ended = next.size == songs.size
                    songs.addAll(next.drop(songs.size))
                    total = if (ended) songs.size else page.total
                    error = null
                }
                .onFailure { error = serverMessage(it, "Couldn't load") }
            loading = false
        }
    }
    LaunchedEffect(artistId) { if (songs.isEmpty()) loadMore() }
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(vertical = 16.dp)) {
        item {
            Column(Modifier.padding(horizontal = sidePadding, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text("Top Songs", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                artist?.let { Text(it, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
        }
        items(songs.size) { i ->
            val t = songs[i]
            if (i >= songs.size - 30) LaunchedEffect(i) { loadMore() }
            TopSongRow(t, i, Modifier.initialFocus(marquee.isTv && (if (opened != null) opened == t.id else i == 0))) {
                opened = t.id
                music.play(songs.toList(), i, source = listOfNotNull(artist, "Top Songs").joinToString(" – "))
            }
        }
        error?.let { e -> item { Text("Couldn't load: $e", Modifier.padding(sidePadding), color = MaterialTheme.colorScheme.error) } }
        if (loading) item { Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
    }
}
