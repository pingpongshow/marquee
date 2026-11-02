package app.marquee.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.QueueMusic
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.CloudOff
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import app.marquee.core.Downloads
import app.marquee.core.Marquee
import app.marquee.core.OfflineContent

/** The Offline Library's route: what phones and tablets show in place of everything else while offline. */
const val OFFLINE_ROUTE = "offline"

/** The items whose files are on this device, as of [entries]. */
private fun onDevice(downloads: Downloads, entries: Map<Long, Downloads.Entry>): List<ItemSummary> =
    entries.values.filter { it.state == Downloads.State.Done && downloads.localFile(it.item.id) != null }.map { it.item }

/** What's downloaded, grouped; recomputed when the downloads change. */
@Composable
private fun rememberOfflineContent(): Pair<OfflineContent, List<ItemSummary>> {
    val downloads = LocalDownloads.current
    val entries by downloads.entries.collectAsState()
    return remember(entries) { onDevice(downloads, entries).let { OfflineContent.of(it) to it } }
}

@Composable
private fun posterUrl(id: Long): String? = LocalDownloads.current.posterFile(id)?.let { android.net.Uri.fromFile(it).toString() }

/** Plays [list] in order from [start]: each video going on to the next, as a playlist does (USER-7). */
private fun playVideos(marquee: Marquee, nav: NavHostController, list: List<ItemSummary>, start: Int = 0) {
    marquee.videoQueue = list.drop(start).map { it.id }
    list.getOrNull(start)?.let { nav.navigate("player/${it.id}") }
}

/**
 * Everything on the device while the server can't be reached (phones and tablets): movies, shows
 * with their episodes, music and downloaded playlists, all playing from their files.
 */
@Composable
fun OfflineLibraryScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    val downloads = LocalDownloads.current
    val (content, all) = rememberOfflineContent()
    val playlistIds by downloads.playlists.collectAsState()
    val infos by downloads.playlistInfo.collectAsState()
    val onDevice = remember(all) { all.map { it.id }.toSet() }
    // Only playlists with something on the device to play.
    val playlists = playlistIds.filter { (_, ids) -> ids.any { it in onDevice } }.keys.map { it to infos[it] }.sortedBy { it.second?.title?.lowercase().orEmpty() }
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(vertical = 16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        item { Text("Offline Library", Modifier.padding(horizontal = sidePadding, vertical = 8.dp), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
        if (content.isEmpty) item {
            Column(Modifier.fillMaxWidth().padding(48.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Icon(Icons.Filled.CloudOff, null, Modifier.size(40.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Text("Nothing downloaded", fontWeight = FontWeight.SemiBold)
                Text("Download movies, episodes and music while you're connected, and they'll be here when you're not.",
                    color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
            }
        }
        if (content.movies.isNotEmpty()) item {
            Shelf("Movies", content.movies, sidePadding, onTitle = null) { i, m ->
                PosterCard(m, posterUrl(m.id), 110.dp, { playVideos(marquee, nav, listOf(m)) }, shape = if (m.type == ItemType.VIDEO) Shape.Wide else Shape.Poster)
            }
        }
        if (content.shows.isNotEmpty()) {
            section("Shows")
            items(content.shows, key = { "show${it.id}" }) { show ->
                val n = show.episodes.size
                OfflineRow(show.title, if (n == 1) "1 episode" else "$n episodes", show.episodes.firstOrNull()?.id, Shape.Wide, 96) { nav.navigate("offlineshow/${show.id}") }
            }
        }
        if (content.albums.isNotEmpty()) {
            section("Albums")
            items(content.albums, key = { "album${it.id}" }) { album ->
                OfflineRow(album.title, album.artist ?: "", album.tracks.firstOrNull()?.id, Shape.Square, 56) { nav.navigate("offlinealbum/${album.id}") }
            }
        }
        if (playlists.isNotEmpty()) {
            section("Playlists")
            items(playlists, key = { "playlist${it.first}" }) { (id, info) ->
                OfflineRow(info?.title?.ifBlank { null } ?: "Playlist", listOfNotNull(info?.ownerName?.let { "Shared by $it" }).joinToString(), null, Shape.Square, 56, Icons.AutoMirrored.Filled.QueueMusic) {
                    nav.navigate("offlineplaylist/$id")
                }
            }
        }
        if (content.songs.isNotEmpty()) {
            section("Songs")
            itemsIndexed(content.songs, key = { _, t -> "song${t.id}" }) { i, t -> OfflineTrackRow(t) { music.play(content.songs, i, source = "Downloads") } }
        }
    }
}

private fun LazyListScope.section(title: String) = item(key = "section-$title") {
    Text(title, Modifier.padding(start = sidePadding, end = sidePadding, top = 16.dp, bottom = 4.dp).semantics { heading() },
        style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
}

/** A show, album or playlist in the list, opening its own page. */
@Composable
private fun OfflineRow(title: String, detail: String, posterOf: Long?, shape: Shape, width: Int, icon: androidx.compose.ui.graphics.vector.ImageVector? = null, onClick: () -> Unit) {
    ListItem(
        headlineContent = { Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis) },
        supportingContent = detail.takeIf { it.isNotEmpty() }?.let { { Text(it, maxLines = 1, overflow = TextOverflow.Ellipsis) } },
        leadingContent = {
            if (icon != null) Icon(icon, null, Modifier.size(32.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            else Artwork(posterOf?.let { posterUrl(it) }, title, shape, Modifier.width(width.dp))
        },
        trailingContent = { Icon(Icons.Filled.ChevronRight, null) },
        colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
        modifier = Modifier.focusCard(onClick),
    )
}

/** A downloaded song: plays on a tap, with its heart and rating (queued until the server's back). */
@Composable
private fun OfflineTrackRow(t: ItemSummary, leading: String? = null, onClick: () -> Unit) {
    ListItem(
        headlineContent = { Text(t.title, maxLines = 1, overflow = TextOverflow.Ellipsis) },
        supportingContent = { Text(listOfNotNull(t.artistCredit ?: t.grandparentTitle, t.parentTitle.takeIf { leading == null }).joinToString(" · "), maxLines = 1, overflow = TextOverflow.Ellipsis) },
        leadingContent = {
            if (leading != null) Text(leading, color = MaterialTheme.colorScheme.onSurfaceVariant)
            else Artwork(posterUrl(t.id), t.title, Shape.Square, Modifier.width(44.dp))
        },
        trailingContent = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                FavoriteButton(t, small = true)
                app.marquee.music.TrackRating(t)
            }
        },
        colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
        modifier = Modifier.focusCard(onClick),
    )
}

/** Play and Shuffle for a page of downloaded tracks or videos. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun OfflineButtons(onPlay: () -> Unit, onShuffle: () -> Unit) {
    FlowRow(Modifier.padding(horizontal = sidePadding, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
        Button(onPlay, Modifier.focusRing()) { Icon(Icons.Filled.PlayArrow, null); Text("Play", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false) }
        OutlinedButton(onShuffle, Modifier.focusRing()) { Icon(Icons.Filled.Shuffle, null); Text("Shuffle", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false) }
    }
}

@Composable
private fun OfflineHeading(title: String, detail: String?) {
    Column(Modifier.padding(horizontal = sidePadding, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(title, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
        detail?.takeIf { it.isNotEmpty() }?.let { Text(it, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}

/** A show's downloaded episodes, by season: each plays on through those after it. */
@Composable
fun OfflineShowScreen(nav: NavHostController, showId: Long) {
    val marquee = LocalMarquee.current
    val (content) = rememberOfflineContent()
    val show = content.shows.firstOrNull { it.id == showId }
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(vertical = 16.dp)) {
        item { OfflineHeading(show?.title ?: "", null) }
        if (show != null) {
            item { OfflineButtons({ playVideos(marquee, nav, show.episodes) }, { playVideos(marquee, nav, show.episodes.shuffled()) }) }
            show.episodes.groupBy { it.parentTitle }.forEach { (season, eps) ->
                season?.let { section(it) }
                items(eps, key = { "ep${it.id}" }) { ep ->
                    ListItem(
                        headlineContent = { Text("${ep.index ?: ""}. ${ep.title}", maxLines = 2, overflow = TextOverflow.Ellipsis) },
                        supportingContent = { Text(listOfNotNull(ep.durationMs?.let { formatTime(it) }, if (isWatched(ep)) "Watched" else null).joinToString(" · ")) },
                        leadingContent = { Artwork(posterUrl(ep.id), ep.title, Shape.Wide, Modifier.width(120.dp)) },
                        colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                        modifier = Modifier.focusCard({ playVideos(marquee, nav, show.episodes, show.episodes.indexOf(ep)) }),
                    )
                }
            }
        }
    }
}

/** An album's downloaded tracks. */
@Composable
fun OfflineAlbumScreen(albumId: Long) {
    val music = LocalMusic.current
    val (content) = rememberOfflineContent()
    val album = content.albums.firstOrNull { it.id == albumId }
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(vertical = 16.dp)) {
        item { OfflineHeading(album?.title ?: "", album?.artist) }
        if (album != null) {
            item { OfflineButtons({ music.play(album.tracks, 0, source = album.title) }, { music.play(album.tracks.shuffled(), 0, source = album.title) }) }
            itemsIndexed(album.tracks, key = { _, t -> "t${t.id}" }) { i, t ->
                OfflineTrackRow(t, leading = "${t.index ?: i + 1}") { music.play(album.tracks, i, source = album.title) }
            }
        }
    }
}

/** A downloaded playlist, in its order: only what's on the device, tracks and videos each playing as they do online. */
@Composable
fun OfflinePlaylistScreen(nav: NavHostController, playlistId: Long) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    val downloads = LocalDownloads.current
    val (_, all) = rememberOfflineContent()
    val infos by downloads.playlistInfo.collectAsState()
    val kept by downloads.playlists.collectAsState()
    val info = infos[playlistId]
    val byId = remember(all) { all.associateBy { it.id } }
    // In the playlist's order; one kept before the order was remembered, as its items were.
    val entries = (info?.items ?: kept[playlistId].orEmpty().toList()).mapNotNull { byId[it] }
    val tracks = entries.filter { it.type == ItemType.TRACK }
    val videos = entries.filter { it.type != ItemType.TRACK }
    val title = info?.title?.ifBlank { null } ?: "Playlist"
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(vertical = 16.dp)) {
        item { OfflineHeading(title, info?.ownerName?.let { "Shared by $it" }) }
        if (entries.isNotEmpty()) item {
            OfflineButtons(
                { if (tracks.isNotEmpty()) music.play(tracks, 0, source = title) else playVideos(marquee, nav, videos) },
                { if (tracks.isNotEmpty()) music.play(tracks.shuffled(), 0, source = title) else playVideos(marquee, nav, videos.shuffled()) },
            )
        }
        items(entries, key = { "e${it.id}" }) { e ->
            if (e.type == ItemType.TRACK) OfflineTrackRow(e) { music.play(tracks, tracks.indexOf(e), source = title) }
            else ListItem(
                headlineContent = { Text(e.title, maxLines = 2, overflow = TextOverflow.Ellipsis) },
                supportingContent = { Text(subtitleFor(e), maxLines = 1, overflow = TextOverflow.Ellipsis) },
                leadingContent = { Artwork(posterUrl(e.id), e.title, Shape.Wide, Modifier.width(96.dp)) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = Modifier.focusCard({ playVideos(marquee, nav, videos, videos.indexOf(e)) }),
            )
        }
    }
}
