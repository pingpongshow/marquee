package app.marquee.ui

import android.text.format.Formatter
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CloudOff
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.DownloadDone
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.ErrorOutline
import androidx.compose.material.icons.filled.Movie
import androidx.compose.material.icons.filled.MusicNote
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import app.marquee.core.Downloads
import coil3.compose.AsyncImage
import kotlinx.coroutines.launch

/** Everything downloaded to this device (M7, MUSIC-14). Works offline. */
@Composable
fun DownloadsScreen(nav: NavHostController) {
    val downloads = LocalDownloads.current
    val music = LocalMusic.current
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val all = downloads.entries.collectAsState().value.values.sortedByDescending { it.addedMs }
    val busy = all.filter { it.state != Downloads.State.Done }
    val videos = all.filter { it.state == Downloads.State.Done && it.item.type != ItemType.TRACK }
    val tracks = all.filter { it.state == Downloads.State.Done && it.item.type == ItemType.TRACK }
        .sortedWith(compareBy({ it.item.grandparentTitle }, { it.item.parentTitle }, { it.item.index ?: 0 }))

    fun open(e: Downloads.Entry) {
        if (e.state != Downloads.State.Done) return
        if (e.item.type == ItemType.TRACK) {
            val album = tracks.filter { it.item.parentId == e.item.parentId }.map { it.item }
            music.play(album, album.indexOfFirst { it.id == e.item.id }.coerceAtLeast(0), source = e.item.parentTitle)
        } else nav.navigate("player/${e.item.id}")
    }

    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp)) {
        item { Text("Downloads", Modifier.padding(horizontal = sidePadding, vertical = 8.dp), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
        if (marquee.isOffline) item {
            Row(Modifier.padding(horizontal = sidePadding, vertical = 6.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Filled.CloudOff, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Text("You're offline. Downloads play without the server; what you watch syncs when you're back.",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        if (all.isEmpty()) item {
            Column(Modifier.fillMaxWidth().padding(48.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Icon(Icons.Filled.Download, null, Modifier.size(40.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Text("No downloads", fontWeight = FontWeight.SemiBold)
                Text("Download movies, episodes and music from their pages to watch and listen offline.",
                    color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
            }
        }
        if (busy.isNotEmpty()) {
            item { Section("In progress") }
            items(busy, key = { "b${it.item.id}" }) { Row(it, downloads, ::open) }
        }
        if (videos.isNotEmpty()) {
            item { Section("Movies and TV") }
            items(videos, key = { "v${it.item.id}" }) { Row(it, downloads, ::open) }
        }
        if (tracks.isNotEmpty()) {
            item {
                Row(Modifier.fillMaxWidth().padding(end = sidePadding), verticalAlignment = Alignment.CenterVertically) {
                    Section("Music", Modifier.weight(1f))
                    TextButton({ music.play(tracks.map { it.item }, 0, source = "Downloads") }) { Text("Play all") }
                    TextButton({ music.play(tracks.map { it.item }.shuffled(), 0, source = "Downloads") }) { Text("Shuffle") }
                }
            }
            items(tracks, key = { "t${it.item.id}" }) { Row(it, downloads, ::open) }
        }
        if (all.isNotEmpty()) item {
            Text("${Formatter.formatShortFileSize(context, downloads.totalBytes)} on this device",
                Modifier.padding(sidePadding), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun Section(title: String, modifier: Modifier = Modifier) =
    Text(title, modifier.padding(start = sidePadding, top = 16.dp, bottom = 6.dp), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)

@Composable
private fun Row(e: Downloads.Entry, downloads: Downloads, open: (Downloads.Entry) -> Unit) {
    val context = LocalContext.current
    val square = e.item.type == ItemType.TRACK
    Row(Modifier.fillMaxWidth().focusCard({ open(e) }).padding(horizontal = sidePadding, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Box(Modifier.width(if (square) 48.dp else 64.dp).height(if (square) 48.dp else 96.dp).clip(RoundedCornerShape(6.dp)).background(Surface2), contentAlignment = Alignment.Center) {
            val poster = downloads.posterFile(e.item.id)
            if (poster != null) AsyncImage(poster, null, contentScale = ContentScale.Crop, modifier = Modifier.matchParentSize())
            else Icon(if (square) Icons.Filled.MusicNote else Icons.Filled.Movie, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(e.item.title, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(subtitle(e), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
            when (e.state) {
                Downloads.State.Preparing, Downloads.State.Downloading -> {
                    LinearProgressIndicator(progress = { e.progress.toFloat() }, Modifier.fillMaxWidth().padding(top = 2.dp))
                    Text("${if (e.state == Downloads.State.Preparing) "Preparing on the server" else "Downloading"}… ${(e.progress * 100).toInt()}%", style = MaterialTheme.typography.labelSmall)
                }
                Downloads.State.Done -> Text(Formatter.formatShortFileSize(context, e.size), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Downloads.State.Failed -> Text(e.error ?: "Failed", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.error, maxLines = 2)
            }
        }
        IconButton({ downloads.remove(e.item.id) }) { Icon(Icons.Filled.Delete, "Delete ${e.item.title}", tint = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}

private fun subtitle(e: Downloads.Entry): String = when (e.item.type) {
    ItemType.EPISODE -> listOfNotNull(e.item.grandparentTitle, e.item.parentTitle).joinToString(" · ")
    ItemType.TRACK -> listOfNotNull(e.item.artistCredit ?: e.item.grandparentTitle, e.item.parentTitle).joinToString(" · ")
    else -> listOfNotNull(e.item.year?.toString(), e.quality.label.takeIf { e.quality != Downloads.Quality.Original }).joinToString(" · ")
}

/** The download control on item pages: pick a quality, then progress or "Downloaded". */
@Composable
fun DownloadButton(item: ItemSummary) {
    val downloads = LocalDownloads.current
    val context = LocalContext.current
    val entries by downloads.entries.collectAsState()
    var menu by remember { mutableStateOf(false) }
    val single = item.type in listOf(ItemType.MOVIE, ItemType.EPISODE, ItemType.VIDEO, ItemType.TRACK)
    val musicContainer = item.type == ItemType.ALBUM || item.type == ItemType.ARTIST
    fun start(q: Downloads.Quality) {
        menu = false
        // The app's scope: leaving the page mustn't stop a season or album halfway.
        downloads.scope.launch {
            runCatching { if (single) downloads.download(item, q) else downloads.downloadAll(item, q) }
                .onFailure { android.widget.Toast.makeText(context.applicationContext, it.message ?: "Couldn't download", android.widget.Toast.LENGTH_LONG).show() }
        }
    }
    val e = entries[item.id]
    Box {
        when {
            single && e != null -> OutlinedButton({ menu = true }, Modifier.focusRing()) {
                Icon(when (e.state) { Downloads.State.Done -> Icons.Filled.DownloadDone; Downloads.State.Failed -> Icons.Filled.ErrorOutline; else -> Icons.Filled.Download }, null)
                Spacer(Modifier.width(6.dp))
                Text(when (e.state) { Downloads.State.Done -> "Downloaded"; Downloads.State.Failed -> "Failed"; else -> "${(e.progress * 100).toInt()}%" })
            }
            item.type == ItemType.TRACK || musicContainer -> OutlinedButton({ start(Downloads.Quality.Original) }, Modifier.focusRing()) {
                Icon(Icons.Filled.Download, null); Spacer(Modifier.width(6.dp)); Text("Download")
            }
            else -> OutlinedButton({ menu = true }, Modifier.focusRing()) {
                Icon(Icons.Filled.Download, null); Spacer(Modifier.width(6.dp))
                Text(when (item.type) { ItemType.SEASON -> "Download Season"; ItemType.SHOW -> "Download All"; else -> "Download" })
            }
        }
        DropdownMenu(menu, { menu = false }) {
            if (single && e != null) DropdownMenuItem({ Text("Delete Download") }, { menu = false; downloads.remove(item.id) })
            else Downloads.Quality.entries.forEach { q -> DropdownMenuItem({ Text(q.label) }, { start(q) }) }
        }
    }
}
